package gitextension

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"

	extension "github.com/paulmooreparks/dinah-extension-go"
)

type Request struct {
	Repo             string                   `json:"repo"`
	Path             string                   `json:"path"`
	PathKey          string                   `json:"pathKey"`
	Paths            []string                 `json:"paths"`
	PathKeys         []string                 `json:"pathKeys"`
	Staged           bool                     `json:"staged"`
	Revision         string                   `json:"revision"`
	Hunk             int                      `json:"hunk"`
	Message          string                   `json:"message"`
	Ref              string                   `json:"ref"`
	Branch           string                   `json:"branch"`
	Remote           string                   `json:"remote"`
	URL              string                   `json:"url"`
	Amend            bool                     `json:"amend"`
	Limit            int                      `json:"limit"`
	IncludeUntracked bool                     `json:"includeUntracked"`
	NewBranch        bool                     `json:"newBranch"`
	Command          *extension.TerminalInput `json:"command"`
}
type File struct {
	Path        string `json:"path"`
	Key         string `json:"key"`
	Original    string `json:"original,omitempty"`
	OriginalKey string `json:"originalKey,omitempty"`
	Index       string `json:"index"`
	Worktree    string `json:"worktree"`
	Conflict    bool   `json:"conflict"`
	Untracked   bool   `json:"untracked"`
}
type Status struct {
	Repo          string `json:"repo"`
	Branch        string `json:"branch"`
	Head          string `json:"head"`
	Upstream      string `json:"upstream"`
	Ahead         int    `json:"ahead"`
	Behind        int    `json:"behind"`
	Files         []File `json:"files"`
	IndexRevision string `json:"indexRevision"`
	Revision      string `json:"revision"`
}
type Repository struct {
	Path  string `json:"path"`
	Name  string `json:"name"`
	Error string `json:"error,omitempty"`
}
type Engine struct {
	mu    sync.Mutex
	locks map[string]*sync.Mutex
}

func New() *Engine { return &Engine{locks: map[string]*sync.Mutex{}} }
func (e *Engine) lock(repo string) func() {
	e.mu.Lock()
	lock := e.locks[repo]
	if lock == nil {
		lock = &sync.Mutex{}
		e.locks[repo] = lock
	}
	e.mu.Unlock()
	lock.Lock()
	return lock.Unlock
}
func hash(text string) string { sum := sha256.Sum256([]byte(text)); return hex.EncodeToString(sum[:]) }

var baseArgs = []string{"--no-pager", "--literal-pathspecs", "-c", "color.ui=false", "-c", "core.quotepath=false", "-c", "core.fsmonitor=false"}

func git(ctx context.Context, h *extension.Host, repo, why string, args ...string) (extension.RunResult, error) {
	out, err := h.Run(ctx, extension.RunInput{Program: "git", Args: append(append([]string{}, baseArgs...), args...), Cwd: repo, Explanation: why, TimeoutSeconds: 120})
	if err != nil {
		return out, err
	}
	if out.Truncated {
		return out, errors.New("Git output exceeded the host limit; narrow the selection")
	}
	return out, nil
}
func checked(ctx context.Context, h *extension.Host, repo, why string, args ...string) (string, error) {
	out, err := git(ctx, h, repo, why, args...)
	if err != nil {
		return "", err
	}
	if out.ExitCode != 0 {
		return "", fmt.Errorf("git %s failed (exit %d): %s", args[0], out.ExitCode, strings.TrimSpace(out.Stderr+out.Stdout))
	}
	return out.Stdout, nil
}
func root(ctx context.Context, h *extension.Host, repo string) (string, error) {
	if repo == "" {
		repo = h.Scope.Workspace.Path
	}
	out, err := checked(ctx, h, repo, "Find the repository root. rev-parse --show-toplevel prints its absolute working-tree path.", "rev-parse", "--show-toplevel")
	if err != nil {
		return "", err
	}
	return strings.TrimRight(out, "\r\n"), nil
}
func validPath(p string) bool {
	return p != "" && !filepath.IsAbs(p) && p != ".." && !strings.HasPrefix(p, "../") && !strings.HasPrefix(p, "..\\") && !strings.ContainsRune(p, 0)
}
func pathValue(r Request) (string, error) {
	p := r.Path
	if r.PathKey != "" {
		data, err := base64.RawURLEncoding.DecodeString(r.PathKey)
		if err != nil {
			return "", err
		}
		p = string(data)
	}
	if !validPath(p) {
		return "", errors.New("choose a repository-relative file")
	}
	return p, nil
}
func pathsValue(r Request) ([]string, error) {
	out := append([]string{}, r.Paths...)
	for _, key := range r.PathKeys {
		data, err := base64.RawURLEncoding.DecodeString(key)
		if err != nil {
			return nil, err
		}
		out = append(out, string(data))
	}
	if len(out) == 0 {
		p, err := pathValue(r)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	for _, p := range out {
		if !validPath(p) {
			return nil, errors.New("invalid repository-relative path")
		}
	}
	return out, nil
}
func ParseStatus(repo, raw string) (Status, error) {
	s := Status{Repo: repo, Files: []File{}}
	records := strings.Split(raw, "\x00")
	for i := 0; i < len(records); i++ {
		row := records[i]
		if row == "" {
			continue
		}
		if strings.HasPrefix(row, "# ") {
			name, value, _ := strings.Cut(strings.TrimPrefix(row, "# "), " ")
			switch name {
			case "branch.oid":
				s.Head = value
			case "branch.head":
				s.Branch = value
			case "branch.upstream":
				s.Upstream = value
			case "branch.ab":
				parts := strings.Fields(value)
				if len(parts) == 2 {
					s.Ahead, _ = strconv.Atoi(strings.TrimPrefix(parts[0], "+"))
					s.Behind, _ = strconv.Atoi(strings.TrimPrefix(parts[1], "-"))
				}
			}
			continue
		}
		file := File{Index: ".", Worktree: "."}
		var p string
		switch row[0] {
		case '?':
			if len(row) < 3 {
				return s, errors.New("invalid untracked record")
			}
			p = row[2:]
			file.Untracked = true
			file.Worktree = "?"
		case '1', '2', 'u':
			count := 9
			if row[0] == '2' {
				count = 10
			}
			if row[0] == 'u' {
				count = 11
				file.Conflict = true
			}
			parts := strings.SplitN(row, " ", count)
			if len(parts) != count || len(parts[1]) != 2 {
				return s, errors.New("invalid porcelain status record")
			}
			file.Index, file.Worktree = parts[1][:1], parts[1][1:]
			p = parts[count-1]
			if row[0] == '2' {
				i++
				if i >= len(records) {
					return s, errors.New("missing rename source")
				}
				file.Original = strings.ToValidUTF8(records[i], "�")
				file.OriginalKey = base64.RawURLEncoding.EncodeToString([]byte(records[i]))
			}
		case '!':
			continue
		default:
			return s, errors.New("unrecognized porcelain status record")
		}
		file.Path = strings.ToValidUTF8(p, "�")
		file.Key = base64.RawURLEncoding.EncodeToString([]byte(p))
		s.Files = append(s.Files, file)
	}
	s.Revision = hash(raw)
	return s, nil
}
func status(ctx context.Context, h *extension.Host, repo string) (Status, error) {
	raw, err := checked(ctx, h, repo, "List staged, unstaged, untracked and conflicted paths. Porcelain v2 is stable machine output; -z separates paths with NUL.", "status", "--porcelain=v2", "--branch", "-z", "--untracked-files=all")
	if err != nil {
		return Status{}, err
	}
	s, err := ParseStatus(repo, raw)
	if err != nil {
		return s, err
	}
	index, err := checked(ctx, h, repo, "Fingerprint staged blob IDs and modes without changing the index.", "diff", "--cached", "--raw", "-z", "--no-abbrev", "--no-ext-diff", "--no-textconv")
	if err != nil {
		return s, err
	}
	s.IndexRevision = hash(s.Head + "\x00" + index)
	return s, nil
}

type Hunk struct {
	Index  int    `json:"index"`
	Header string `json:"header"`
	Text   string `json:"text"`
}
type Diff struct {
	Path          string `json:"path"`
	Key           string `json:"key"`
	Staged        bool   `json:"staged"`
	Patch         string `json:"patch"`
	Revision      string `json:"revision"`
	Hunks         []Hunk `json:"hunks"`
	Binary        bool   `json:"binary"`
	CanStageHunks bool   `json:"canStageHunks"`
}

var hunkLine = regexp.MustCompile(`(?m)^@@ -[^\n]+`)

func ParseDiff(p, patch string, staged bool) Diff {
	d := Diff{Path: strings.ToValidUTF8(p, "�"), Key: base64.RawURLEncoding.EncodeToString([]byte(p)), Staged: staged, Patch: patch, Revision: hash(p + "\x00" + strconv.FormatBool(staged) + "\x00" + patch), Hunks: []Hunk{}}
	ranges := hunkLine.FindAllStringIndex(patch, -1)
	for i, span := range ranges {
		end := len(patch)
		if i+1 < len(ranges) {
			end = ranges[i+1][0]
		}
		d.Hunks = append(d.Hunks, Hunk{Index: i, Header: patch[span[0]:span[1]], Text: patch[span[0]:end]})
	}
	d.Binary = strings.Contains(patch, "Binary files ") || strings.Contains(patch, "GIT binary patch")
	d.CanStageHunks = len(d.Hunks) > 0 && !d.Binary && !strings.Contains(patch, "new file mode ") && !strings.Contains(patch, "deleted file mode ") && !strings.Contains(patch, "rename from ") && !strings.Contains(patch, "old mode ")
	return d
}
func diff(ctx context.Context, h *extension.Host, repo, p string, staged bool) (Diff, error) {
	args := []string{"diff", "--no-ext-diff", "--no-textconv", "--no-renames", "--src-prefix=a/", "--dst-prefix=b/"}
	if staged {
		args = append(args, "--cached")
	}
	args = append(args, "--", p)
	patch, err := checked(ctx, h, repo, "Show the selected file's original unified patch without external diff or text-conversion programs.", args...)
	if err != nil {
		return Diff{}, err
	}
	if patch == "" && !staged {
		tracked, e := git(ctx, h, repo, "Check whether the selected path is tracked.", "ls-files", "--error-unmatch", "--", p)
		if e != nil {
			return Diff{}, e
		}
		if tracked.ExitCode != 0 {
			out, e := git(ctx, h, repo, "Preview an untracked file against an empty file. Exit 1 means differences were found.", "diff", "--no-index", "--no-ext-diff", "--no-textconv", "--", "/dev/null", p)
			if e != nil {
				return Diff{}, e
			}
			if out.ExitCode > 1 {
				return Diff{}, errors.New(out.Stderr)
			}
			patch = out.Stdout
		}
	}
	return ParseDiff(p, patch, staged), nil
}
func (e *Engine) Handlers() map[string]extension.Handler {
	out := map[string]extension.Handler{}
	for _, a := range Manifest().Actions {
		name := a.Name
		out[name] = func(ctx context.Context, h *extension.Host, raw json.RawMessage) (any, error) {
			var in Request
			if err := json.Unmarshal(raw, &in); err != nil {
				return nil, err
			}
			return e.Call(ctx, h, name, in)
		}
	}
	return out
}
func (e *Engine) Call(ctx context.Context, h *extension.Host, action string, in Request) (any, error) {
	if action == "repositories" {
		seen := map[string]bool{}
		repos := []Repository{}
		sources := append([]extension.Source{{Path: h.Scope.Workspace.Path, Name: h.Scope.Workspace.Name}}, h.Scope.Workspace.Sources...)
		for _, source := range sources {
			if seen[source.Path] {
				continue
			}
			seen[source.Path] = true
			p, err := root(ctx, h, source.Path)
			if err != nil {
				repos = append(repos, Repository{Path: source.Path, Name: source.Name, Error: err.Error()})
				continue
			}
			if p != source.Path && seen[p] {
				continue
			}
			seen[p] = true
			repos = append(repos, Repository{Path: p, Name: source.Name})
		}
		return map[string]any{"repositories": repos}, nil
	}
	if action == "command-log" {
		var rows []json.RawMessage
		err := h.Call(ctx, "host.commandlog", struct{}{}, &rows)
		return map[string]any{"entries": rows}, err
	}
	if action == "terminal" {
		if in.Command == nil || in.Command.Program != "git" {
			return nil, errors.New("choose a command from the Command Log")
		}
		return map[string]bool{"prepared": true}, h.Call(ctx, "host.terminal", in.Command, nil)
	}
	if action == "init" {
		if in.Repo == "" {
			return nil, errors.New("choose a workspace source folder")
		}
		out, err := checked(ctx, h, in.Repo, "Initialize Git in the selected workspace folder.", "init")
		return map[string]string{"output": out}, err
	}
	repo, err := root(ctx, h, in.Repo)
	if err != nil {
		return nil, err
	}
	defer e.lock(repo)()
	switch action {
	case "status":
		return status(ctx, h, repo)
	case "diff":
		p, err := pathValue(in)
		if err != nil {
			return nil, err
		}
		return diff(ctx, h, repo, p, in.Staged)
	case "stage-hunk", "unstage-hunk":
		p, err := pathValue(in)
		if err != nil {
			return nil, err
		}
		staged := action == "unstage-hunk"
		d, err := diff(ctx, h, repo, p, staged)
		if err != nil {
			return nil, err
		}
		if in.Revision == "" || d.Revision != in.Revision {
			return nil, errors.New("the diff changed; refresh it and select the hunk again")
		}
		if !d.CanStageHunks || in.Hunk < 0 || in.Hunk >= len(d.Hunks) {
			return nil, errors.New("this change requires staging or unstaging the whole file")
		}
		begin := hunkLine.FindStringIndex(d.Patch)[0]
		patch := d.Patch[:begin] + d.Hunks[in.Hunk].Text
		args := []string{"apply", "--cached", "--check"}
		if staged {
			args = append(args, "--reverse")
		}
		result, err := h.Run(ctx, extension.RunInput{Program: "git", Args: append(append([]string{}, baseArgs...), args...), Cwd: repo, Stdin: patch, Explanation: "Validate the complete selected hunk against the current index before changing it."})
		if err != nil {
			return nil, err
		}
		if result.ExitCode != 0 {
			return nil, errors.New(result.Stderr)
		}
		args = []string{"apply", "--cached"}
		if staged {
			args = append(args, "--reverse")
		}
		result, err = h.Run(ctx, extension.RunInput{Program: "git", Args: append(append([]string{}, baseArgs...), args...), Cwd: repo, Stdin: patch, Explanation: "Apply only this complete hunk to the index; leave working-tree text unchanged."})
		if err != nil {
			return nil, err
		}
		if result.ExitCode != 0 {
			return nil, errors.New(result.Stderr)
		}
		return status(ctx, h, repo)
	case "stage", "unstage":
		paths, err := pathsValue(in)
		if err != nil {
			return nil, err
		}
		args := []string{"add", "-A", "--"}
		if action == "unstage" {
			s, err := status(ctx, h, repo)
			if err != nil {
				return nil, err
			}
			if s.Head == "(initial)" {
				args = []string{"rm", "--cached", "--ignore-unmatch", "--"}
			} else {
				args = []string{"restore", "--staged", "--"}
			}
		}
		if _, err = checked(ctx, h, repo, "Change index entries for the selected literal paths only.", append(args, paths...)...); err != nil {
			return nil, err
		}
		return status(ctx, h, repo)
	case "commit":
		if strings.TrimSpace(in.Message) == "" {
			return nil, errors.New("a commit message is required")
		}
		s, err := status(ctx, h, repo)
		if err != nil {
			return nil, err
		}
		if in.Revision == "" || s.IndexRevision != in.Revision {
			return nil, errors.New("the index or HEAD changed; review staged changes before committing")
		}
		args := []string{"commit", "-m", in.Message}
		if in.Amend {
			args = append(args, "--amend")
		}
		out, err := checked(ctx, h, repo, "Create a commit from the reviewed index. Git hooks and signing configuration remain active.", args...)
		return map[string]string{"output": out}, err
	case "history":
		return history(ctx, h, repo, in)
	case "commit-detail":
		ref, err := verifiedCommit(ctx, h, repo, in.Ref)
		if err != nil {
			return nil, err
		}
		out, err := checked(ctx, h, repo, "Show one verified commit and its patch without external converters.", "show", "--no-ext-diff", "--no-textconv", "--format=fuller", "--stat", "--patch", ref, "--")
		return map[string]string{"text": out}, err
	case "branches":
		out, err := checked(ctx, h, repo, "List local and remote branches with stable field separators.", "for-each-ref", "--format=%(refname:short)%00%(objectname)%00%(HEAD)%00%(upstream:short)%00%(upstream:track)", "refs/heads", "refs/remotes")
		return map[string]any{"rows": nulRows(out, 5)}, err
	case "remotes":
		out, err := checked(ctx, h, repo, "List configured remotes and their fetch/push URLs.", "remote", "-v")
		return map[string]string{"text": out}, err
	case "stashes":
		out, err := checked(ctx, h, repo, "List stash identities and descriptions.", "stash", "list", "--format=%gd%x00%H%x00%gs")
		return map[string]any{"rows": nulRows(out, 3)}, err
	case "tags":
		out, err := checked(ctx, h, repo, "List tag names, object IDs and subjects.", "for-each-ref", "--format=%(refname:short)%00%(objectname)%00%(subject)", "refs/tags")
		return map[string]any{"rows": nulRows(out, 3)}, err
	case "worktrees":
		out, err := checked(ctx, h, repo, "List worktrees in machine-readable NUL-delimited porcelain format.", "worktree", "list", "--porcelain", "-z")
		return map[string]string{"text": strings.ReplaceAll(out, "\x00", "\n")}, err
	}
	return mutate(ctx, h, repo, action, in)
}
func nulRows(raw string, columns int) [][]string {
	rows := [][]string{}
	for _, line := range strings.Split(strings.TrimSuffix(raw, "\n"), "\n") {
		row := strings.SplitN(line, "\x00", columns)
		if len(row) == columns {
			rows = append(rows, row)
		}
	}
	return rows
}
func verifiedCommit(ctx context.Context, h *extension.Host, repo, ref string) (string, error) {
	if ref == "" || strings.ContainsRune(ref, 0) {
		return "", errors.New("choose a commit")
	}
	out, err := checked(ctx, h, repo, "Resolve the revision to a commit object without treating it as an option.", "rev-parse", "--verify", "--end-of-options", ref+"^{commit}")
	return strings.TrimSpace(out), err
}
func history(ctx context.Context, h *extension.Host, repo string, in Request) (any, error) {
	limit := in.Limit
	if limit <= 0 {
		limit = 80
	}
	if limit > 200 {
		return nil, errors.New("history is limited to 200 commits")
	}
	args := []string{"log", "-n", strconv.Itoa(limit), "--format=%H%x00%h%x00%P%x00%an%x00%aI%x00%s%x00%D%x00", "-z"}
	if in.Ref != "" {
		ref, err := verifiedCommit(ctx, h, repo, in.Ref)
		if err != nil {
			return nil, err
		}
		args = append(args, ref)
	}
	args = append(args, "--")
	raw, err := checked(ctx, h, repo, "Read bounded history with explicit NUL-separated fields.", args...)
	if err != nil {
		return nil, err
	}
	parts := strings.Split(raw, "\x00")
	rows := [][]string{}
	for i := 0; i+6 < len(parts); {
		if parts[i] == "" {
			i++
			continue
		}
		rows = append(rows, parts[i:i+7])
		i += 7
	}
	graph, err := checked(ctx, h, repo, "Draw Git's own branch graph as readable text.", "log", "--graph", "--oneline", "--decorate", "-n", strconv.Itoa(limit), "--")
	return map[string]any{"rows": rows, "graph": graph}, err
}

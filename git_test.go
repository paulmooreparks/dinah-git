package gitextension

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	extension "github.com/paulmooreparks/dinah-extension-go"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func runGit(t *testing.T, repo string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = repo
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %s %v", args, out, err)
	}
	return string(out)
}
func testHost(t *testing.T, repo string) *extension.Host {
	t.Helper()
	canonical, err := filepath.EvalSymlinks(repo)
	if err != nil {
		t.Fatal(err)
	}
	repo = canonical
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer fixture" {
			http.Error(w, "no scope", 403)
			return
		}
		var request extension.HostRequest
		json.NewDecoder(r.Body).Decode(&request)
		if request.Tool == "host.resolve" {
			var in struct {
				Path string `json:"path"`
			}
			json.Unmarshal(request.Input, &in)
			rel, err := filepath.Rel(repo, in.Path)
			if err != nil || !filepath.IsLocal(rel) {
				json.NewEncoder(w).Encode(extension.HostResponse{Error: "outside fixture"})
				return
			}
			data, _ := json.Marshal(map[string]string{"path": in.Path})
			json.NewEncoder(w).Encode(extension.HostResponse{Result: data})
			return
		}
		if request.Tool != "host.run" {
			json.NewEncoder(w).Encode(extension.HostResponse{Error: "unsupported test host tool"})
			return
		}
		var in extension.RunInput
		json.Unmarshal(request.Input, &in)
		cwd, err := filepath.EvalSymlinks(in.Cwd)
		if err != nil {
			json.NewEncoder(w).Encode(extension.HostResponse{Error: err.Error()})
			return
		}
		relative, err := filepath.Rel(repo, cwd)
		if err != nil || strings.HasPrefix(relative, "..") {
			json.NewEncoder(w).Encode(extension.HostResponse{Error: "outside fixture workspace"})
			return
		}
		cmd := exec.CommandContext(r.Context(), in.Program, in.Args...)
		cmd.Dir = in.Cwd
		cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_TERMINAL_PROMPT=0")
		cmd.Stdin = strings.NewReader(in.Stdin)
		var stderr strings.Builder
		cmd.Stderr = &stderr
		out, err := cmd.Output()
		result := extension.RunResult{Stdout: string(out), Stderr: stderr.String()}
		if err != nil {
			result.ExitCode = 1
			var exit *exec.ExitError
			if e, ok := err.(*exec.ExitError); ok {
				exit = e
				result.ExitCode = exit.ExitCode()
			} else {
				result.Stderr += err.Error()
			}
		}
		data, _ := json.Marshal(result)
		json.NewEncoder(w).Encode(extension.HostResponse{Result: data})
	}))
	t.Cleanup(server.Close)
	return &extension.Host{Scope: extension.Scope{URL: server.URL + "/host", Token: "fixture", Workspace: extension.Workspace{ID: "fixture", Path: repo}}}
}
func fixture(t *testing.T) (string, *extension.Host) {
	t.Helper()
	repo := t.TempDir()
	canonical, err := filepath.EvalSymlinks(repo)
	if err != nil {
		t.Fatal(err)
	}
	repo = canonical
	runGit(t, repo, "init", "-b", "main")
	runGit(t, repo, "config", "user.name", "Fixture")
	runGit(t, repo, "config", "user.email", "fixture@example.invalid")
	runGit(t, repo, "config", "commit.gpgSign", "false")
	runGit(t, repo, "config", "core.autocrlf", "false")
	return repo, testHost(t, repo)
}
func TestRepositoryOperations(t *testing.T) {
	repo, h := fixture(t)
	ctx := context.Background()
	engine := New()
	os.WriteFile(filepath.Join(repo, "file.txt"), []byte("initial\n"), 0600)
	runGit(t, repo, "add", "--", "file.txt")
	runGit(t, repo, "commit", "-m", "initial")
	invoke := func(action string, in Request) any {
		t.Helper()
		in.Repo = repo
		out, err := engine.Call(ctx, h, action, in)
		if err != nil {
			t.Fatalf("%s: %v", action, err)
		}
		return out
	}
	invoke("branch-create", Request{Branch: "feature"})
	invoke("switch", Request{Branch: "feature"})
	os.WriteFile(filepath.Join(repo, "feature.txt"), []byte("feature\n"), 0600)
	runGit(t, repo, "add", "--", "feature.txt")
	runGit(t, repo, "commit", "-m", "feature")
	invoke("switch", Request{Branch: "main"})
	invoke("merge", Request{Ref: "feature"})
	invoke("branch-delete", Request{Branch: "feature"})
	invoke("tag-create", Request{Ref: "v-fixture", Message: "Test tag"})
	if got := invoke("tags", Request{}).(map[string]any)["rows"].([][]string); len(got) != 1 {
		t.Fatal("tag missing")
	}
	invoke("tag-delete", Request{Ref: "v-fixture"})
	os.WriteFile(filepath.Join(repo, "file.txt"), []byte("stash this\n"), 0600)
	invoke("stash-save", Request{Message: "fixture"})
	invoke("stash-apply", Request{Ref: "stash@{0}"})
	runGit(t, repo, "restore", "--", "file.txt")
	invoke("stash-pop", Request{Ref: "stash@{0}"})
	runGit(t, repo, "restore", "--", "file.txt")
	bare := filepath.Join(repo, "remote.git")
	runGit(t, repo, "init", "--bare", bare)
	invoke("remote-add", Request{Remote: "origin", URL: bare})
	invoke("push", Request{Remote: "origin", Branch: "main"})
	invoke("fetch", Request{Remote: "origin"})
	invoke("pull", Request{Remote: "origin", Branch: "main"})
	invoke("remote-remove", Request{Remote: "origin"})
	worktrees := filepath.Join(repo, "worktrees")
	os.Mkdir(worktrees, 0700)
	target := filepath.Join(worktrees, "test")
	invoke("worktree-add", Request{Path: target, Branch: "worktree-test", NewBranch: true})
	os.WriteFile(filepath.Join(target, "untracked.txt"), []byte("preserve"), 0600)
	if _, err := engine.Call(ctx, h, "worktree-remove", Request{Repo: repo, Path: target}); err == nil {
		t.Fatal("removed dirty worktree")
	}
	os.Remove(filepath.Join(target, "untracked.txt"))
	invoke("worktree-remove", Request{Path: target})
	invoke("branch-delete", Request{Branch: "worktree-test"})
}

func TestNonUTF8FilenameThroughHostProtocol(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("raw-byte filesystem regression runs on Linux; JSON byte tests run on every platform")
	}
	repo, h := fixture(t)
	name := "invalid-\xff.txt"
	if err := os.WriteFile(filepath.Join(repo, name), []byte("content\n"), 0600); err != nil {
		t.Fatal(err)
	}
	engine := New()
	value, err := engine.Call(context.Background(), h, "status", Request{Repo: repo})
	if err != nil {
		t.Fatal(err)
	}
	s := value.(Status)
	if len(s.Files) != 1 {
		t.Fatal("missing file")
	}
	key := s.Files[0].Key
	bytes, _ := base64.RawURLEncoding.DecodeString(key)
	if string(bytes) != name {
		t.Fatal("raw filename changed across host JSON")
	}
	if _, err = engine.Call(context.Background(), h, "stage", Request{Repo: repo, PathKeys: []string{key}}); err != nil {
		t.Fatal(err)
	}
	if out := runGit(t, repo, "ls-files", "-z"); out != name+"\x00" {
		t.Fatalf("staged wrong path %q", out)
	}
}
func TestStageHunkAndRejectStaleDiff(t *testing.T) {
	repo, h := fixture(t)
	lines := []string{}
	for i := 0; i < 45; i++ {
		lines = append(lines, fmt.Sprintf("line %d", i))
	}
	p := filepath.Join(repo, "file.txt")
	os.WriteFile(p, []byte(strings.Join(lines, "\n")+"\n"), 0600)
	runGit(t, repo, "add", "--", "file.txt")
	runGit(t, repo, "commit", "-m", "initial")
	lines[3] = "first change"
	lines[35] = "second change"
	os.WriteFile(p, []byte(strings.Join(lines, "\n")+"\n"), 0600)
	e := New()
	result, err := e.Call(context.Background(), h, "diff", Request{Repo: repo, Path: "file.txt"})
	if err != nil {
		t.Fatal(err)
	}
	d := result.(Diff)
	if len(d.Hunks) != 2 || !d.CanStageHunks {
		t.Fatalf("hunks: %+v", d.Hunks)
	}
	_, err = e.Call(context.Background(), h, "stage-hunk", Request{Repo: repo, Path: "file.txt", Revision: d.Revision, Hunk: 0})
	if err != nil {
		t.Fatal(err)
	}
	cached := runGit(t, repo, "diff", "--cached")
	if !strings.Contains(cached, "first change") || strings.Contains(cached, "second change") {
		t.Fatal("staged the wrong hunk")
	}
	_, err = e.Call(context.Background(), h, "stage-hunk", Request{Repo: repo, Path: "file.txt", Revision: d.Revision, Hunk: 1})
	if err == nil || !strings.Contains(err.Error(), "diff changed") {
		t.Fatalf("stale diff was accepted: %v", err)
	}
	staged, err := diff(context.Background(), h, repo, "file.txt", true)
	if err != nil {
		t.Fatal(err)
	}
	_, err = e.Call(context.Background(), h, "unstage-hunk", Request{Repo: repo, Path: "file.txt", Revision: staged.Revision, Hunk: 0})
	if err != nil {
		t.Fatal(err)
	}
	if runGit(t, repo, "diff", "--cached") != "" {
		t.Fatal("unstage hunk retained index change")
	}
	if data, _ := os.ReadFile(p); !strings.Contains(string(data), "second change") {
		t.Fatal("staging changed working text")
	}
}
func TestLiteralPathsCommitAndBranches(t *testing.T) {
	repo, h := fixture(t)
	e := New()
	for _, name := range []string{"--option name.txt", "[literal].txt"} {
		os.WriteFile(filepath.Join(repo, name), []byte(name), 0600)
	}
	s, err := status(context.Background(), h, repo)
	if err != nil || len(s.Files) != 2 {
		t.Fatalf("status %+v %v", s, err)
	}
	keys := []string{}
	for _, f := range s.Files {
		keys = append(keys, f.Key)
	}
	if _, err = e.Call(context.Background(), h, "stage", Request{Repo: repo, PathKeys: keys}); err != nil {
		t.Fatal(err)
	}
	s, err = status(context.Background(), h, repo)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = e.Call(context.Background(), h, "commit", Request{Repo: repo, Revision: "stale", Message: "wrong"}); err == nil {
		t.Fatal("stale commit accepted")
	}
	if _, err = e.Call(context.Background(), h, "commit", Request{Repo: repo, Revision: s.IndexRevision, Message: "first"}); err != nil {
		t.Fatal(err)
	}
	if _, err = e.Call(context.Background(), h, "branch-create", Request{Repo: repo, Branch: "feature"}); err != nil {
		t.Fatal(err)
	}
	if _, err = e.Call(context.Background(), h, "switch", Request{Repo: repo, Branch: "feature"}); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(runGit(t, repo, "branch", "--show-current")); got != "feature" {
		t.Fatal(got)
	}
	result, err := history(context.Background(), h, repo, Request{})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.(map[string]any)["rows"].([][]string)) != 1 {
		t.Fatal("history did not parse exactly one commit")
	}
}
func TestPorcelainPathsAndRenameRecords(t *testing.T) {
	raw := "# branch.oid abc\x00# branch.head main\x002 R. N... 100644 100644 100644 abc def R100 new name\x00old\nname\x00? " + string([]byte{'x', 255}) + "\x00u UU N... 100644 100644 100644 100644 a b c conflict\x00"
	s, err := ParseStatus("repo", raw)
	if err != nil || len(s.Files) != 3 {
		t.Fatalf("%+v %v", s, err)
	}
	if s.Files[0].Original != "old\nname" || !s.Files[2].Conflict {
		t.Fatal("rename/conflict parsing lost data")
	}
	bytes, err := base64.RawURLEncoding.DecodeString(s.Files[1].Key)
	if err != nil || len(bytes) != 2 || bytes[1] != 255 {
		t.Fatal("non-UTF8 path identity lost")
	}
}

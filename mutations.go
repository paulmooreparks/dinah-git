package gitextension

import (
	"context"
	"errors"
	"fmt"
	extension "github.com/paulmooreparks/dinah-extension-go"
	"strings"
)

func name(value, what string) (string, error) {
	if value == "" || strings.HasPrefix(value, "-") || strings.ContainsAny(value, "\x00\r\n") {
		return "", fmt.Errorf("choose a valid %s", what)
	}
	return value, nil
}
func mutate(ctx context.Context, h *extension.Host, repo, action string, in Request) (any, error) {
	var args []string
	why := ""
	branch := func() (string, error) {
		b, err := name(in.Branch, "branch")
		if err != nil {
			return "", err
		}
		_, err = checked(ctx, h, repo, "Validate the branch name using Git's own rules.", "check-ref-format", "--branch", b)
		return b, err
	}
	remote := func() (string, error) {
		r := in.Remote
		if r == "" {
			r = "origin"
		}
		return name(r, "remote")
	}
	switch action {
	case "switch":
		b, err := branch()
		if err != nil {
			return nil, err
		}
		args = []string{"switch", "--no-guess", b}
		why = "Switch branches without discarding local changes."
	case "branch-create":
		b, err := branch()
		if err != nil {
			return nil, err
		}
		args = []string{"branch", "--", b}
		if in.Ref != "" {
			ref, err := verifiedCommit(ctx, h, repo, in.Ref)
			if err != nil {
				return nil, err
			}
			args = append(args, ref)
		}
		why = "Create a local branch at the selected commit."
	case "branch-delete":
		b, err := branch()
		if err != nil {
			return nil, err
		}
		args = []string{"branch", "--delete", "--", b}
		why = "Delete a merged local branch. Unmerged branches are refused."
	case "fetch", "pull", "push":
		r, err := remote()
		if err != nil {
			return nil, err
		}
		args = []string{action}
		if action == "pull" {
			args = append(args, "--ff-only")
		}
		args = append(args, "--", r)
		if in.Branch != "" {
			b, err := branch()
			if err != nil {
				return nil, err
			}
			args = append(args, b)
		}
		why = "Contact the selected remote. Pull accepts fast-forward updates only; push never forces."
	case "remote-add":
		r, err := name(in.Remote, "remote")
		if err != nil {
			return nil, err
		}
		u, err := name(in.URL, "remote URL")
		if err != nil {
			return nil, err
		}
		if strings.HasPrefix(u, "ext::") {
			return nil, errors.New("external-helper URLs are not supported")
		}
		args = []string{"remote", "add", "--", r, u}
		why = "Add the remote name and URL without contacting it."
	case "remote-remove":
		r, err := name(in.Remote, "remote")
		if err != nil {
			return nil, err
		}
		args = []string{"remote", "remove", "--", r}
		why = "Remove a remote's local configuration."
	case "stash-save":
		args = []string{"stash", "push"}
		if in.IncludeUntracked {
			args = append(args, "--include-untracked")
		}
		if in.Message != "" {
			args = append(args, "-m", in.Message)
		}
		why = "Save local changes in a stash."
	case "stash-apply", "stash-pop", "stash-drop":
		ref, err := name(in.Ref, "stash reference")
		if err != nil {
			return nil, err
		}
		if !strings.HasPrefix(ref, "stash@{") || !strings.HasSuffix(ref, "}") {
			return nil, errors.New("choose a stash from the list")
		}
		args = []string{"stash", strings.TrimPrefix(action, "stash-"), ref}
		why = "Apply, pop or remove the explicitly selected stash."
	case "tag-create":
		tag, err := name(in.Ref, "tag")
		if err != nil {
			return nil, err
		}
		if _, err = checked(ctx, h, repo, "Validate a tag reference.", "check-ref-format", "refs/tags/"+tag); err != nil {
			return nil, err
		}
		args = []string{"tag"}
		if in.Message != "" {
			args = append(args, "-a", "-m", in.Message)
		}
		args = append(args, "--", tag)
		why = "Create a tag without replacing an existing tag."
	case "tag-delete":
		tag, err := name(in.Ref, "tag")
		if err != nil {
			return nil, err
		}
		args = []string{"tag", "--delete", "--", tag}
		why = "Delete the selected local tag. Remote tags remain unchanged."
	case "worktree-add", "worktree-remove":
		var target struct {
			Path string `json:"path"`
		}
		if err := h.Call(ctx, "host.resolve", map[string]any{"path": in.Path, "create": action == "worktree-add"}, &target); err != nil {
			return nil, err
		}
		args = []string{"worktree", strings.TrimPrefix(action, "worktree-")}
		if action == "worktree-add" {
			if in.NewBranch {
				b, err := branch()
				if err != nil {
					return nil, err
				}
				args = append(args, "-b", b)
			}
			args = append(args, "--", target.Path)
			if !in.NewBranch && in.Branch != "" {
				b, err := branch()
				if err != nil {
					return nil, err
				}
				args = append(args, b)
			}
		} else {
			args = append(args, "--", target.Path)
		}
		why = "Manage a worktree inside the allowed workspace sources; no force removal is used."
	case "merge", "rebase", "cherry-pick", "revert":
		ref, err := verifiedCommit(ctx, h, repo, in.Ref)
		if err != nil {
			return nil, err
		}
		args = []string{action}
		if action == "merge" {
			args = append(args, "--no-edit")
		}
		if action == "revert" {
			args = append(args, "--no-edit")
		}
		args = append(args, ref)
		why = "Apply the selected verified commit or branch. Conflicts remain available for explicit resolution."
	case "merge-abort", "rebase-abort", "rebase-continue", "cherry-pick-abort":
		parts := strings.Split(action, "-")
		command, flag := parts[0], parts[1]
		if action == "cherry-pick-abort" {
			command, flag = "cherry-pick", "abort"
		}
		args = []string{command, "--" + flag}
		why = "Continue or abort the existing Git operation."
	default:
		return nil, errors.New("unknown Git action")
	}
	out, err := checked(ctx, h, repo, why, args...)
	return map[string]string{"output": out}, err
}

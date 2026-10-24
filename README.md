# Git for Dinah

This extension adds source control to Dinah through the public [Dinah Extension Go SDK](https://github.com/paulmooreparks/dinah-extension-go). It compiles without Dinah's source and invokes the installed Git executable through host services.

The Git pane follows the active workspace and finds repositories at its execution folder and local source roots. Its sections cover working changes, staged changes, history, branches, remotes, stashes, tags, worktrees and the command log. Unified and side-by-side previews preserve ordinary text selection. Complete hunks can be staged or unstaged while the working file remains unchanged. Stale diff and index revisions are refused before applying a hunk or committing.

Repository Actions provides safe branch creation/deletion, switching, fetch, fast-forward-only pull, push, stash operations, local tags, worktrees, merge, rebase, cherry-pick, revert and operation aborts. Git handles hooks, signing, authentication and conflicts. The extension does not silently discard work, force push or force-remove dirty worktrees. Resolve conflicts in an editor or Terminal, then refresh the pane.

Every Git process records the actual argument array, explanation, output and exit code in the host's Command Log. Run in Terminal prepares a command without executing it. Enable Show Me First in Dinah's Extensions pane to review each command before it runs. Process-based agent actions use Dinah's consent and once-only replay rules.

## Build and package

Go 1.25 or later and Git are required. Run `go test ./...` and `go vet ./...` before packaging.

```powershell
pwsh tools/build.ps1 -SigningKey C:/private/publisher.private-key
go run github.com/paulmooreparks/dinah-extension-go/cmd/dinah-extension zip ./dist/extension ./dist/dinah-git.zip
dinah ext install ./dist/dinah-git.zip
```

The script builds amd64 backends for Windows, Linux and macOS. The package must be signed before installation. Native extensions run under your account; inspect the publisher key and requested permissions before installing. No signing key belongs in this repository.

## Boundaries

History reads at most 200 commits, command output is bounded by the host, and large diff previews show at most 200,000 characters. Binary files, additions, deletions, renames and mode changes use whole-file staging. Hunk staging operates on complete Git hunks and checks the current patch revision. It does not implement arbitrary line staging.

This release does not provide file-tree decorations, editor gutter marks, recursive repository discovery, interactive rebase editing, credential management or a replacement for Git's conflict resolver. Configured Git hooks and tools may ask for input; use Terminal when an operation needs an interactive session. The branch graph uses Git's readable graph output.

The implementation was written for this extension. Other Git clients informed the design; no GPL client code is included.

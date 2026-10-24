// Package gitextension implements Git through Dinah's published host contract.
package gitextension

import (
	"encoding/json"
	extension "github.com/paulmooreparks/dinah-extension-go"
)

const ID = "com.parkscomputing.dinah-git"
const Version = "0.1.0"

var InputSchema = json.RawMessage(`{"type":"object","additionalProperties":false,"properties":{"repo":{"type":"string","maxLength":4096},"path":{"type":"string","maxLength":4096},"pathKey":{"type":"string","maxLength":8192},"paths":{"type":"array","maxItems":500,"items":{"type":"string","maxLength":4096}},"pathKeys":{"type":"array","maxItems":500,"items":{"type":"string","maxLength":8192}},"staged":{"type":"boolean"},"revision":{"type":"string","maxLength":128},"hunk":{"type":"integer","minimum":0,"maximum":10000},"message":{"type":"string","maxLength":65536},"ref":{"type":"string","maxLength":1024},"branch":{"type":"string","maxLength":1024},"remote":{"type":"string","maxLength":1024},"url":{"type":"string","maxLength":4096},"amend":{"type":"boolean"},"limit":{"type":"integer","minimum":1,"maximum":200},"includeUntracked":{"type":"boolean"},"newBranch":{"type":"boolean"},"command":{"type":"object","additionalProperties":false,"required":["program","args","cwd"],"properties":{"program":{"const":"git"},"argsBase64":{"type":"array","maxItems":256,"items":{"type":"string","maxLength":16384}},"args":{"type":"array","maxItems":256,"items":{"type":"string","maxLength":8192}},"cwd":{"type":"string","maxLength":4096}}}}}`)
var OutputSchema = json.RawMessage(`{"type":"object"}`)

func Manifest() extension.Manifest {
	m := extension.Manifest{ID: ID, Version: Version, Contract: 1, Title: "Git", Description: "Source control, readable diffs, history and real Git commands.", Publisher: extension.Publisher{Name: "Paul Parks"}, Backends: map[string]string{"windows": "bin/dinah-git.exe", "linux": "bin/dinah-git", "darwin": "bin/dinah-git-darwin"}, Permissions: []string{"workspace.read", "process.run:git", "terminal.open"}, Views: []extension.View{{ID: "git", Title: "Git", Glyph: "script", Page: "web/index.html", Pane: true}}, Commands: []extension.Command{{ID: "git", Title: "Git", View: "git"}}}
	names := [][2]string{{"repositories", "Find repositories"}, {"status", "Repository status"}, {"diff", "File diff"}, {"history", "Commit history"}, {"commit-detail", "Commit details"}, {"branches", "Branches"}, {"remotes", "Remotes"}, {"stashes", "Stashes"}, {"tags", "Tags"}, {"worktrees", "Worktrees"}, {"command-log", "Command log"}, {"terminal", "Prepare command in Terminal"}, {"stage", "Stage files"}, {"unstage", "Unstage files"}, {"stage-hunk", "Stage hunk"}, {"unstage-hunk", "Unstage hunk"}, {"commit", "Commit changes"}, {"switch", "Switch branch"}, {"branch-create", "Create branch"}, {"branch-delete", "Delete merged branch"}, {"fetch", "Fetch remote"}, {"pull", "Pull with fast-forward only"}, {"push", "Push branch"}, {"remote-add", "Add remote"}, {"remote-remove", "Remove remote"}, {"stash-save", "Save stash"}, {"stash-apply", "Apply stash"}, {"stash-pop", "Pop stash"}, {"stash-drop", "Drop stash"}, {"tag-create", "Create tag"}, {"tag-delete", "Delete tag"}, {"worktree-add", "Add worktree"}, {"worktree-remove", "Remove clean worktree"}, {"merge", "Merge branch"}, {"merge-abort", "Abort merge"}, {"rebase", "Rebase branch"}, {"rebase-continue", "Continue rebase"}, {"rebase-abort", "Abort rebase"}, {"cherry-pick", "Cherry-pick commit"}, {"cherry-pick-abort", "Abort cherry-pick"}, {"revert", "Revert commit"}, {"init", "Initialize repository"}}
	for _, entry := range names {
		permissions := []string{"workspace.read", "process.run:git"}
		if entry[0] == "command-log" {
			permissions = []string{"workspace.read"}
		}
		if entry[0] == "terminal" {
			permissions = append(permissions, "terminal.open")
		}
		replay := "once"
		if !extension.Effectful(permissions) {
			replay = "pure"
		}
		m.Actions = append(m.Actions, extension.Action{Name: entry[0], Title: entry[1], Description: entry[1] + " using Git's documented command-line interface. Commands and output are retained in the Command Log.", InputSchema: InputSchema, OutputSchema: OutputSchema, Permissions: permissions, Replay: replay})
	}
	return m
}

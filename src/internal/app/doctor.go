package app

import (
	"os"

	"github.com/ichava/icon-sets-sync/src/internal/core/sources"
	"github.com/ichava/icon-sets-sync/src/internal/core/toolpath"
)

// Tool is one external program a pipeline may run, and whether it is here.
type Tool struct {
	Name string `json:"name"`
	// Why says what needs it.
	Why string `json:"why"`
	// Optional tools only matter on a fallback path.
	Optional bool   `json:"optional"`
	Path     string `json:"path,omitempty"`
	Found    bool   `json:"found"`
	// OnPath is false for a tool found only in the usual install
	// directories: it works, and PATH is missing that directory.
	OnPath bool   `json:"on_path"`
	Detail string `json:"detail,omitempty"`
}

// Tools checks every external program a sync can run.
func Tools() []Tool {
	out := []Tool{
		lookTool("git", "creates the sync branch and commit in a real run, and reads each pack's branch and status", false),
		lookTool("gh", "opens the pull request after a real run (git.open_pr)", false),
	}
	npm := Tool{Name: "npm", Optional: true,
		Why: "only a fallback: npm packages are fetched from registry.npmjs.org without it; npm pack runs when that fetch fails, and in the manifest-pack audit"}
	if s, err := sources.FindNpm(); err == nil {
		npm.Found, npm.Path = true, s.Found
		if p, onPath, err := toolpath.LookPath("npm"); err == nil && p == s.Found {
			npm.OnPath = onPath
		}
	} else {
		npm.Detail = err.Error()
	}
	return append(out, npm)
}

func lookTool(name, why string, optional bool) Tool {
	t := Tool{Name: name, Why: why, Optional: optional}
	p, onPath, err := toolpath.LookPath(name)
	if err != nil {
		t.Detail = name + " is not on PATH (" + os.Getenv("PATH") + ") or in the usual install directories"
		return t
	}
	t.Found, t.Path, t.OnPath = true, p, onPath
	return t
}

// MissingTools lists the required tools that are not found.
func MissingTools(tools []Tool) []Tool {
	var out []Tool
	for _, t := range tools {
		if !t.Found && !t.Optional {
			out = append(out, t)
		}
	}
	return out
}

package web

import (
	"go/build"
	"strings"
	"testing"
)

const module = "github.com/ichava/icon-sets-sync/src"

// The web layer must never reach the CLI's or the terminal UI's packages:
// they hold process-wide state (flags, the ui writer, the TUI model) that a
// long-lived server serving several browsers cannot share.
func TestWebDoesNotImportTheTerminalFrontEnds(t *testing.T) {
	forbidden := []string{module + "/internal/cli", module + "/internal/ui", module + "/internal/tui"}
	seen := map[string]bool{}
	var walk func(path, dir string)
	walk = func(path, dir string) {
		if seen[path] {
			return
		}
		seen[path] = true
		pkg, err := build.Import(path, dir, 0)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		for _, imp := range pkg.Imports {
			if strings.HasPrefix(imp, module+"/") {
				walk(imp, pkg.Dir)
			}
		}
	}
	walk(module+"/internal/web", ".")
	if len(seen) < 5 {
		t.Fatalf("walked %d packages; the import walk is not finding the module", len(seen))
	}
	for _, f := range forbidden {
		if seen[f] {
			t.Errorf("internal/web depends on %s", f)
		}
	}
	if !seen[module+"/internal/app"] {
		t.Error("the walk did not reach internal/app, so it proves nothing")
	}
}

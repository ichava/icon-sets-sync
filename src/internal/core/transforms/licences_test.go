package transforms

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ichava/icon-sets-sync/src/internal/core/pipeline"
)

func TestIsLicenceFile(t *testing.T) {
	cases := []struct {
		name string
		want bool
	}{
		{"LICENSE", true},
		{"LICENSE.md", true},
		{"license.txt", true},
		{"LICENCE", true},
		{"LICENSE-GRAPHICS", true},
		{"LICENSE_FONT", true},
		{"COPYING", true},
		{"NOTICE", true},
		{"NOTICE.txt", true},
		{"OFL.txt", true},
		{"README.md", false},
		{"package.json", false},
		{"licenses.json", false},
		{"unlicensed.txt", false},
		{"icon-license.svg", false},
	}
	for _, c := range cases {
		if got := IsLicenceFile(c.name); got != c.want {
			t.Errorf("IsLicenceFile(%q) = %v, want %v", c.name, got, c.want)
		}
	}
}

// TestFindLicencesReadsTheRootOnly: a LICENSE deep in an upstream tree belongs
// to a dependency or a fixture, and a directory named LICENSE is not a file.
func TestFindLicencesReadsTheRootOnly(t *testing.T) {
	root := t.TempDir()
	write := func(rel, body string) {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("LICENSE", "mit")
	write("NOTICE", "notice")
	write("README.md", "readme")
	write("icons/a.svg", "<svg/>")
	write("node_modules/dep/LICENSE", "not ours")
	if err := os.MkdirAll(filepath.Join(root, "LICENSES"), 0o755); err != nil {
		t.Fatal(err)
	}

	ctx := &pipeline.Context{PipelineName: "t", WorkingDir: t.TempDir(),
		Config: map[string]any{}, Extras: map[string]any{}, Metrics: map[string]any{}}
	ctx.SetFetchedPath(root)

	if err := (FindLicences{}).Execute(ctx); err != nil {
		t.Fatal(err)
	}
	raw, ok := ctx.String(pipeline.KeyLicenceFiles)
	if !ok {
		t.Fatal("licence_files not set")
	}
	var names []string
	for _, f := range strings.Split(raw, "\n") {
		names = append(names, filepath.Base(f))
	}
	if got := strings.Join(names, ","); got != "LICENSE,NOTICE" {
		t.Errorf("found %s, want LICENSE,NOTICE", got)
	}
	// The fetched path is untouched: this stage only records.
	if p, _ := ctx.FetchedPath(); p != root {
		t.Errorf("fetched_path moved to %s", p)
	}
}

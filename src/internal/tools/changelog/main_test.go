package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestCheck(t *testing.T) {
	cases := []struct {
		name, body string
		code       int
		want       string
	}{
		{"ordered", "# C\n\n## [Unreleased]\n\n## [0.2.0] - x\n\n## [0.1.10] - x\n\n## [0.1.9]\n\n## [Archived history]\n\n### [0.1.0]\n",
			0, ": 5 sections, correctly ordered"},
		{"out of order", "## [Unreleased]\n\n## [0.1.9]\n\n## [0.1.10]\n",
			1, "::error::  [0.1.9] is listed above [0.1.10], but is the lower version."},
		{"unreleased not first", "## [0.2.0]\n\n## [Unreleased]\n",
			1, "::error::  [Unreleased] is section 2, not the first."},
		{"duplicates", "## [Unreleased]\n\n## [Unreleased]\n\n## [0.1.0]\n\n## [0.1.0]\n",
			1, "duplicate section headings: [Unreleased], [0.1.0]. Each branch"},
		{"no headings", "# Changelog\n", 1, `has no "## [version]" headings`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := write(t, t.TempDir(), "CHANGELOG.md", c.body)
			var out bytes.Buffer
			if got := runCheck(p, &out); got != c.code {
				t.Errorf("exit %d, want %d\n%s", got, c.code, out.String())
			}
			if !strings.Contains(out.String(), c.want) {
				t.Errorf("output %q lacks %q", out.String(), c.want)
			}
		})
	}
	var out bytes.Buffer
	if runCheck(filepath.Join(t.TempDir(), "nope.md"), &out) != 1 || !strings.Contains(out.String(), "not found") {
		t.Errorf("a missing file must fail: %q", out.String())
	}
}

// The repository's own CHANGELOG passes, as the CI job requires.
func TestCheckTheRepositoryChangelog(t *testing.T) {
	var out bytes.Buffer
	if code := runCheck(filepath.Join("..", "..", "..", "..", "CHANGELOG.md"), &out); code != 0 {
		t.Fatalf("CHANGELOG.md fails the order check:\n%s", out.String())
	}
}

func TestClose(t *testing.T) {
	const before = "# Changelog\n\n## [Unreleased]\n\n### Added\n\n- a thing\n\n## [0.1.0] - 2026-01-01\n\n- old\n"
	const after = "# Changelog\n\n## [Unreleased]\n\n## [0.2.0] - 2026-11-04\n\n### Added\n\n- a thing\n\n## [0.1.0] - 2026-01-01\n\n- old\n"
	dir := t.TempDir()
	cl := write(t, dir, "CHANGELOG.md", before)
	mf := write(t, dir, "manifest.json", `{".": "0.2.0"}`)
	gho := filepath.Join(dir, "out")

	var log bytes.Buffer
	if code := runClose(cl, mf, gho, "2026-11-04", &log); code != 0 {
		t.Fatalf("exit %d: %s", code, log.String())
	}
	got, _ := os.ReadFile(cl)
	if string(got) != after {
		t.Errorf("closed text:\n%s\nwant:\n%s", got, after)
	}
	// A second run finds the section and changes nothing.
	if code := runClose(cl, mf, gho, "2026-11-05", &log); code != 0 {
		t.Fatalf("second run exit %d", code)
	}
	again, _ := os.ReadFile(cl)
	if string(again) != after {
		t.Error("a second run rewrote the file")
	}
	outputs, _ := os.ReadFile(gho)
	if string(outputs) != "version=0.2.0\nchanged=true\nversion=0.2.0\nchanged=false\n" {
		t.Errorf("GITHUB_OUTPUT = %q", outputs)
	}

	for name, body := range map[string]string{
		"empty":     "## [Unreleased]\n\n\n## [0.1.0]\n",
		"two heads": "## [Unreleased]\n- a\n## [Unreleased]\n- b\n",
		"none":      "## [0.1.0]\n",
	} {
		p := write(t, t.TempDir(), "CHANGELOG.md", body)
		if code := runClose(p, mf, filepath.Join(t.TempDir(), "o"), "2026-11-04", &log); code != 1 {
			t.Errorf("%s: exit %d, want 1", name, code)
		}
	}
}

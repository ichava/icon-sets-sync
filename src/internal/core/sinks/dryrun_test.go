package sinks

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/ichava/icon-sets-sync/src/internal/core/pipeline"
)

// snapshotTree maps every file under dir (outside .git) to its bytes, so two
// snapshots compare equal only when nothing was added, removed or changed.
func snapshotTree(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, path)
		out[filepath.ToSlash(rel)] = string(b)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// gitStatus is `git status --porcelain` in dir, or "" when git is absent.
func gitStatus(t *testing.T, dir string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", dir, "status", "--porcelain", "--untracked-files=all").Output()
	if err != nil {
		return ""
	}
	return string(out)
}

// fakePack builds a pack checkout with one committed set, an untracked file
// beside it, and a licence directory. Committed when git is on PATH, so the
// tracked/untracked split is real.
func fakePack(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "icon-sets-demo")
	writeSVGs(t, root, map[string]string{
		"resources/assets/svg/files/demo/kept.svg":   "<svg/>",
		"resources/assets/svg/files/demo/edited.svg": "<svg>old</svg>",
		"resources/assets/svg/files/demo/gone.svg":   "<svg/>",
		"resources/assets/svg/licenses/demo/LICENSE": "old licence",
		"composer.json": "{}",
	})
	if _, err := exec.LookPath("git"); err == nil {
		for _, args := range [][]string{
			{"init", "-q"},
			{"add", "-A"},
			{"-c", "user.email=t@example.invalid", "-c", "user.name=t", "commit", "-q", "-m", "seed"},
		} {
			if out, err := exec.Command("git", append([]string{"-C", root}, args...)...).CombinedOutput(); err != nil {
				t.Fatalf("git %v: %v\n%s", args, err, out)
			}
		}
	}
	// Untracked, like the 17 SVGs a dry run once left in icon-sets-openmoji.
	writeSVGs(t, root, map[string]string{"resources/assets/svg/files/demo/untracked.svg": "<svg>mine</svg>"})
	return root
}

// TestDryRunLeavesThePackByteIdentical is the bug a dry run of
// icon-sets-openmoji hit: it wrote 17 new SVGs and modified tracked ones. A
// dry run must report the diff and write nothing into the pack.
func TestDryRunLeavesThePackByteIdentical(t *testing.T) {
	cases := []struct {
		name string
		sink pipeline.Sink
	}{
		{"Filesystem", Filesystem{Root: "{pack_root}/resources/assets/svg/files/demo"}},
		{"Filesystem incremental", Filesystem{Root: "{pack_root}/resources/assets/svg/files/demo", Incremental: true}},
		{"LicenceTexts copy", LicenceTexts{Root: "{pack_root}/resources/assets/svg/licenses/demo", Set: "demo"}},
		{"LicenceTexts missing", LicenceTexts{Root: "{pack_root}/resources/assets/svg/licenses/demo", Set: "demo", MissingReason: "none upstream"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			pack := fakePack(t)
			source := writeSVGs(t, filepath.Join(t.TempDir(), "src"), map[string]string{
				"kept.svg": "<svg/>", "edited.svg": "<svg>new</svg>", "new.svg": "<svg/>",
			})
			ctx := ctxFor(t, source)
			ctx.Config[pipeline.ConfigPackRoot] = pack
			ctx.Config[pipeline.ConfigDryRun] = true
			if c.name == "LicenceTexts copy" {
				lic := filepath.Join(source, "LICENSE")
				if err := os.WriteFile(lic, []byte("new licence"), 0o644); err != nil {
					t.Fatal(err)
				}
				ctx.SetString(pipeline.KeyLicenceFiles, lic)
			} else {
				ctx.SetString(pipeline.KeyLicenceFiles, "")
			}

			before, status := snapshotTree(t, pack), gitStatus(t, pack)
			if err := c.sink.Execute(ctx); err != nil {
				t.Fatal(err)
			}
			if after := snapshotTree(t, pack); !reflect.DeepEqual(before, after) {
				t.Errorf("a dry run changed the pack:\nbefore %v\nafter  %v", before, after)
			}
			if got := gitStatus(t, pack); got != status {
				t.Errorf("git status changed:\nbefore %q\nafter  %q", status, got)
			}
		})
	}
}

// The diff a dry run reports is the one a real run would make.
func TestDryRunStillReportsTheDiff(t *testing.T) {
	pack := fakePack(t)
	source := writeSVGs(t, filepath.Join(t.TempDir(), "src"), map[string]string{
		"kept.svg": "<svg/>", "edited.svg": "<svg>new</svg>", "new.svg": "<svg/>",
	})
	ctx := ctxFor(t, source)
	ctx.Config[pipeline.ConfigPackRoot] = pack
	ctx.Config[pipeline.ConfigDryRun] = true
	if err := (Filesystem{Root: "{pack_root}/resources/assets/svg/files/demo"}).Execute(ctx); err != nil {
		t.Fatal(err)
	}
	m, _ := ctx.Metrics["filesystem"].(map[string]any)
	// The untracked SVG counts as shipped today: a real run would wipe it.
	got := map[string]any{"added": m["added"], "removed": m["removed"], "changed": m["changed"], "dry_run": m["dry_run"]}
	want := map[string]any{"added": 1, "removed": 2, "changed": 1, "dry_run": true}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("dry-run diff = %v, want %v", got, want)
	}
	if m["would_write"] != 3 || m["copied"] != 0 {
		t.Errorf("would_write = %v, copied = %v; want 3 and 0", m["would_write"], m["copied"])
	}
}

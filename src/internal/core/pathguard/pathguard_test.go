package pathguard

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestJoinKeepsAnOrdinaryPathUnderItsRoot(t *testing.T) {
	abs := t.TempDir()
	root := abs
	for _, tc := range []struct {
		elem []string
		want string
	}{
		{nil, abs},
		{[]string{"icons/home.svg"}, filepath.Join(abs, "icons", "home.svg")},
		{[]string{"set", "pkg"}, filepath.Join(abs, "set", "pkg")},
		{[]string{"./a/./b"}, filepath.Join(abs, "a", "b")},
		{[]string{"icons/"}, filepath.Join(abs, "icons")},
		{[]string{"./"}, abs},
		{[]string{"a..b.svg"}, filepath.Join(abs, "a..b.svg")},
	} {
		got, err := Join(root, tc.elem...)
		if err != nil {
			t.Errorf("Join(%v): %v", tc.elem, err)
			continue
		}
		if got != tc.want {
			t.Errorf("Join(%v) = %q, want %q", tc.elem, got, tc.want)
		}
	}
}

func TestJoinRefusesTraversal(t *testing.T) {
	root := t.TempDir()
	for _, elem := range [][]string{
		{"../escape.svg"},
		{"../../etc/passwd"},
		{"a/../../escape.svg"},
		{"a/../b"},
		{".."},
		{"set", ".."},
		{"/absolute/escape.svg"},
		{filepath.Join(root, "x")},
		{`..\escape.svg`},
		{`a\..\..\escape.svg`},
	} {
		if got, err := Join(root, elem...); err == nil {
			t.Errorf("Join(%q) = %q, want it refused", elem, got)
		} else if !errors.Is(err, ErrEscape) {
			t.Errorf("Join(%q): %v, want ErrEscape", elem, err)
		}
	}
	if _, err := Join("", "x"); !errors.Is(err, ErrEscape) {
		t.Errorf("an empty root: %v, want ErrEscape", err)
	}
}

func TestJoinRefusesASymlinkThatLeavesTheRoot(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "root")
	outside := filepath.Join(dir, "outside")
	for _, d := range []string{root, outside, filepath.Join(root, "real")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(outside, filepath.Join(root, "out")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	must(t, os.Symlink(filepath.Join(root, "real"), filepath.Join(root, "in")))
	must(t, os.Symlink(filepath.Join(outside, "missing.svg"), filepath.Join(root, "dangling.svg")))
	must(t, os.WriteFile(filepath.Join(outside, "secret.svg"), []byte("x"), 0o644))
	must(t, os.Symlink(filepath.Join(outside, "secret.svg"), filepath.Join(root, "file.svg")))

	for _, elem := range []string{"out", "out/new.svg", "out/deep/new.svg", "dangling.svg", "file.svg"} {
		if got, err := Join(root, elem); err == nil {
			t.Errorf("Join(%q) = %q through a link out of the root", elem, got)
		} else if !errors.Is(err, ErrEscape) {
			t.Errorf("Join(%q): %v, want ErrEscape", elem, err)
		}
	}
	// A link that stays inside the root is fine.
	if _, err := Join(root, "in", "new.svg"); err != nil {
		t.Errorf("a link inside the root was refused: %v", err)
	}
	// A root reached through a symlink is still the root.
	alias := filepath.Join(dir, "alias")
	must(t, os.Symlink(root, alias))
	if _, err := Join(alias, "real", "x.svg"); err != nil {
		t.Errorf("a root reached through a link was refused: %v", err)
	}
}

func TestJoinKeepsARelativeRootRelative(t *testing.T) {
	got, err := Join(filepath.Join("testdata-not-there", "root"), "a.svg")
	if err != nil || got != filepath.Join("testdata-not-there", "root", "a.svg") {
		t.Errorf("Join of a relative root = %q, %v", got, err)
	}
}

func TestJoinAcceptsARootThatDoesNotExistYet(t *testing.T) {
	root := filepath.Join(t.TempDir(), "later")
	if _, err := Join(root, "a", "b.svg"); err != nil {
		t.Errorf("a root not yet created: %v", err)
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

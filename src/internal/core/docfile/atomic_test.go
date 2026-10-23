package docfile

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestWriteAtomicReplacesAndKeepsTheMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pack.json")
	if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := WriteAtomic(path, []byte("new")); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != "new" {
		t.Fatalf("content %q, err %v", got, err)
	}
	if runtime.GOOS != "windows" {
		info, _ := os.Stat(path)
		if info.Mode().Perm() != 0o600 {
			t.Errorf("mode %v, want 0600 kept", info.Mode().Perm())
		}
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("temporary files left behind: %v", entries)
	}
}

func TestWriteAtomicCreatesANewFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "new.json")
	if err := WriteAtomic(path, []byte("{}")); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); string(got) != "{}" {
		t.Fatalf("content %q", got)
	}
}

func TestWriteAtomicLeavesTheTargetOnFailure(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs a non-root POSIX permission check")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "pack.json")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chmod(dir, 0o755) }()
	if err := WriteAtomic(path, []byte("new")); err == nil {
		t.Fatal("a write into a read-only directory succeeded")
	}
	if got, _ := os.ReadFile(path); string(got) != "old" {
		t.Fatalf("target changed to %q", got)
	}
}

func TestWriteAtomicRemovesTheTempFileWhenTheRenameFails(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pack.json")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	renameFile = func(string, string) error { return errors.New("disk full") }
	defer func() { renameFile = os.Rename }()

	if err := WriteAtomic(path, []byte("new")); err == nil {
		t.Fatal("a failed rename reported success")
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 || entries[0].Name() != "pack.json" {
		t.Fatalf("left behind: %v", entries)
	}
	if got, _ := os.ReadFile(path); string(got) != "old" {
		t.Fatalf("target changed to %q", got)
	}
}

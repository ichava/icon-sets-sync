package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/ichava/icon-sets-sync/src/internal/core/manifestpack"
)

func sprintf(format string, a ...any) string { return fmt.Sprintf(format, a...) }

// copyFetcher "downloads" a package by copying a prepared directory, so the
// audit's own scoring runs against real bytes without npm.
type copyFetcher map[string]string

func (f copyFetcher) Fetch(_ context.Context, pkg, dest string) (string, error) {
	src, ok := f[pkg]
	if !ok {
		return "", errors.New("no such package")
	}
	// NpmFetcher extracts under dest/x/package; the resolver looks there.
	dest = filepath.Join(dest, "x", "package")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return "", err
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return "", err
	}
	for _, e := range entries {
		b, err := os.ReadFile(filepath.Join(src, e.Name()))
		if err != nil {
			return "", err
		}
		if err := os.WriteFile(filepath.Join(dest, e.Name()), b, 0o644); err != nil {
			return "", err
		}
	}
	return "1.2.3", nil
}

func bundledFixture(t *testing.T) (configDir, upstream string) {
	t.Helper()
	root := t.TempDir()
	packRoot := filepath.Join(root, "icon-sets-bundled")
	set := filepath.Join(packRoot, "resources", "assets", "svg", "files", "demo-icons")
	upstream = filepath.Join(root, "upstream")
	for _, d := range []string{set, upstream} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for i := range 3 {
		body := fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><path d="M%d 0h1"/></svg>`, i)
		for _, d := range []string{set, upstream} {
			if err := os.WriteFile(filepath.Join(d, fmt.Sprintf("icon-%d.svg", i)), []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	configDir = filepath.Join(root, "config")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	pack := `{"name": "icon-sets-bundled", "pack": "ichava/icon-sets-bundled", "pack_root": "` + packRoot + `",
  "current_version": "1.0.0", "version_check_url": "https://example.invalid/latest",
  "source": {"type": "npm", "package": "demo"}, "sinks": []}`
	if err := os.WriteFile(filepath.Join(configDir, BundledPack+".json"), []byte(pack), 0o644); err != nil {
		t.Fatal(err)
	}
	return configDir, upstream
}

func TestAuditBundledScoresAgainstTheCommittedBytes(t *testing.T) {
	configDir, upstream := bundledFixture(t)
	var started int
	var done []string
	results, err := AuditBundled(context.Background(), AuditOptions{
		SetDone:   func(r manifestpack.Resolution) { done = append(done, r.Set) },
		ConfigDir: configDir,
		Set:       "demo-icons",
		Package:   "demo-pkg",
		WorkDir:   t.TempDir(),
		Fetcher:   copyFetcher{"demo-pkg": upstream},
		Starting:  func(n int, _ string) { started = n },
	})
	if err != nil {
		t.Fatal(err)
	}
	if started != 1 || len(results) != 1 || len(done) != 1 || done[0] != "demo-icons" {
		t.Fatalf("started %d, results %+v", started, results)
	}
	r := results[0]
	if r.Confidence != manifestpack.ConfidenceExact || r.Identical != 3 || r.Package != "demo-pkg" {
		t.Fatalf("resolution %+v", r)
	}
}

func TestAuditBundledPackageNeedsASet(t *testing.T) {
	configDir, _ := bundledFixture(t)
	_, err := AuditBundled(context.Background(), AuditOptions{ConfigDir: configDir, Package: "x", Fetcher: copyFetcher{}})
	if !IsUsage(err) {
		t.Fatalf("want a usage error, got %v", err)
	}
}

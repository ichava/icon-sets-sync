package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// v0.1.0 to v0.1.2 were published from history rewritten on 2026-11-13. The
// Go proxy and sum.golang.org cache them for good, so the next release must be
// above v0.1.2 and those three must stay retracted.
func TestNextReleaseSkipsTheBurnedVersions(t *testing.T) {
	var cfg struct {
		Packages map[string]struct {
			ReleaseAs string `json:"release-as"`
		} `json:"packages"`
	}
	readJSON(t, "release-please-config.json", &cfg)
	var manifest map[string]string
	readJSON(t, ".release-please-manifest.json", &manifest)

	// release-please releases release-as when it is set, and otherwise bumps the manifest's
	// version, so whichever applies must already be past the burned range.
	next, from := cfg.Packages["."].ReleaseAs, `release-please-config.json packages["."].release-as`
	if next == "" {
		next, from = manifest["."], `.release-please-manifest.json ["."]`
	}
	if !atLeast(t, next, [3]int{0, 1, 3}) {
		t.Errorf("%s = %q; the next release must be v0.1.3 or later", from, next)
	}

	mod, err := os.ReadFile(filepath.Join(filepath.FromSlash(repoRoot), "src", "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	var retracts []string
	for _, line := range strings.Split(string(mod), "\n") {
		if strings.HasPrefix(line, "retract ") {
			retracts = append(retracts, line)
		}
	}
	var found bool
	for _, line := range retracts {
		if strings.HasPrefix(line, "retract [v0.1.0, v0.1.2] //") && strings.Contains(line, "rewritten") {
			found = true
		}
	}
	if !found {
		t.Errorf("src/go.mod must retract [v0.1.0, v0.1.2] with a comment naming the rewritten history; found %d retract lines: %q", len(retracts), retracts)
	}
}

func readJSON(t *testing.T, name string, v any) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(filepath.FromSlash(repoRoot), name))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, v); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
}

// atLeast reports whether version (x.y.z) is at or above floor.
func atLeast(t *testing.T, version string, floor [3]int) bool {
	t.Helper()
	parts := strings.Split(version, ".")
	if len(parts) != 3 {
		t.Fatalf("version %q is not x.y.z", version)
	}
	for i, part := range parts {
		n, err := strconv.Atoi(part)
		if err != nil {
			t.Fatalf("version %q: %v", version, err)
		}
		if n != floor[i] {
			return n > floor[i]
		}
	}
	return true
}

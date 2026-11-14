package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// v0.1.0 to v0.1.2 were published from history rewritten on 2026-11-13. The
// Go proxy and sum.golang.org cache them for good, so the next release must be
// above v0.1.2 and those three must stay retracted.
func TestNextReleaseSkipsTheBurnedVersions(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(filepath.FromSlash(repoRoot), "release-please-config.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Packages map[string]struct {
			ReleaseAs string `json:"release-as"`
		} `json:"packages"`
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatal(err)
	}
	if got := cfg.Packages["."].ReleaseAs; got != "0.1.3" {
		t.Errorf(`release-please-config.json packages["."].release-as = %q, want "0.1.3"`, got)
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

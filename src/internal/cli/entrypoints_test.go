package cli

import (
	"os"
	"path/filepath"
	"testing"

	"go.yaml.in/yaml/v3"
)

// icon-sets-sync ships one binary, plus the Windows launcher below. The short alias was the same program
// (cli.Execute) under a second name and doubled every build; the owner
// retired it on 2026-11-04. A second entrypoint under src/cmd, or a second
// binary in the GoReleaser config, has to come back by changing this test on
// purpose.

const primaryBinary = "icon-sets-sync"

// windowsLauncher is not an alias but a different program: a GUI-subsystem
// launcher that opens no console and starts the CLI's `serve` (owner, option A,
// 2026-11-11). It is built by `make cross` like the macOS .app and the Linux
// .desktop entry, never by GoReleaser, so TestGoReleaserBuildsOneBinary still
// holds.
const windowsLauncher = primaryBinary + "-launcher"

func TestOnlyOneEntrypoint(t *testing.T) {
	entries, err := os.ReadDir(filepath.Join(filepath.FromSlash(repoRoot), "src", "cmd"))
	if err != nil {
		t.Fatalf("reading src/cmd: %v", err)
	}
	dirs := 0
	for _, e := range entries {
		if !e.IsDir() {
			continue // .DS_Store and the like are not entrypoints
		}
		dirs++
		if e.Name() != primaryBinary && e.Name() != windowsLauncher {
			t.Errorf("src/cmd/%s: the only entrypoints are src/cmd/%s and the Windows launcher src/cmd/%s", e.Name(), primaryBinary, windowsLauncher)
		}
	}
	if dirs == 0 {
		t.Fatal("src/cmd holds no entrypoint directory")
	}
}

func TestGoReleaserBuildsOneBinary(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(filepath.FromSlash(repoRoot), "src", ".goreleaser.yaml"))
	if err != nil {
		t.Fatalf("reading src/.goreleaser.yaml: %v", err)
	}
	var cfg struct {
		Builds []struct {
			ID     string `yaml:"id"`
			Main   string `yaml:"main"`
			Binary string `yaml:"binary"`
		} `yaml:"builds"`
		Universal []struct {
			ID           string   `yaml:"id"`
			IDs          []string `yaml:"ids"`
			NameTemplate string   `yaml:"name_template"`
		} `yaml:"universal_binaries"`
		Archives []struct {
			ID  string   `yaml:"id"`
			IDs []string `yaml:"ids"`
		} `yaml:"archives"`
		NFPMs []struct {
			ID  string   `yaml:"id"`
			IDs []string `yaml:"ids"`
		} `yaml:"nfpms"`
	}
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("parsing src/.goreleaser.yaml: %v", err)
	}
	if len(cfg.Builds) != 1 {
		t.Errorf("builds: %d entries, want exactly 1 (%s)", len(cfg.Builds), primaryBinary)
	}
	for _, b := range cfg.Builds {
		if b.ID != primaryBinary || b.Binary != primaryBinary || b.Main != "./cmd/"+primaryBinary {
			t.Errorf("build %q (binary %q, main %q): only %s is built", b.ID, b.Binary, b.Main, primaryBinary)
		}
	}
	if len(cfg.Universal) != 1 {
		t.Errorf("universal_binaries: %d entries, want exactly 1", len(cfg.Universal))
	}
	for _, u := range cfg.Universal {
		if u.NameTemplate != primaryBinary {
			t.Errorf("universal binary %q is named %q, want %s", u.ID, u.NameTemplate, primaryBinary)
		}
		for _, id := range u.IDs {
			if id != primaryBinary {
				t.Errorf("universal binary %q takes build %q", u.ID, id)
			}
		}
	}
	if len(cfg.Archives) == 0 {
		t.Fatal("archives: none found: the config is not being read")
	}
	for _, a := range cfg.Archives {
		for _, id := range a.IDs {
			if id != primaryBinary {
				t.Errorf("archive %q takes build %q", a.ID, id)
			}
		}
	}
	for _, n := range cfg.NFPMs {
		for _, id := range n.IDs {
			if id != primaryBinary {
				t.Errorf("package %q takes build %q", n.ID, id)
			}
		}
	}
}

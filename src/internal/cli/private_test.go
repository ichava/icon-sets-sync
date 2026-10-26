package cli

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/ichava/icon-sets-sync/src/internal/core/config"
)

var (
	shippedConfig = filepath.Join("..", "..", "..", "config")
	privateConfig = filepath.Join("..", "core", "config", "testdata", "private-config")
)

func resetPrivateDir(t *testing.T) {
	t.Helper()
	t.Cleanup(func() { config.SetPrivateConfigDir("") })
}

// The flag merges a private pack, its config and its set manifest into the
// shipped ones: validate reports it beside every public pack.
func TestPrivateConfigDirFlagLoadsAPrivatePack(t *testing.T) {
	isolate(t)
	resetPrivateDir(t)
	out, code := run(t, "--config-dir", shippedConfig, "--private-config-dir", privateConfig, "config", "validate")
	if code != ExitOK {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	for _, want := range []string{"and the private config dir " + privateConfig, "icon-sets-tabler", "icon-sets-flag", "icon-sets-emoji",
		filepath.Join(privateConfig, "icon-sets-example.json") + " (recipe manifest-pack)",
		`"recipe": "bundled" is deprecated and will be removed in 0.2.0`} {
		if !strings.Contains(out, want) {
			t.Errorf("validate did not report %s:\n%s", want, out)
		}
	}
}

func TestPrivateConfigDirEnvironmentLoadsAPrivatePack(t *testing.T) {
	isolate(t)
	resetPrivateDir(t)
	t.Setenv("ICHAVA_PRIVATE_CONFIG_DIR", privateConfig)
	out, code := run(t, "--config-dir", shippedConfig, "list-packs")
	if code != ExitOK || !strings.Contains(out, "icon-sets-example") {
		t.Fatalf("exit %d, want the private pack listed:\n%s", code, out)
	}
}

// Without it, the shipped configs carry no private pack at all.
func TestNoPrivateConfigDirMeansNoPrivatePack(t *testing.T) {
	isolate(t)
	resetPrivateDir(t)
	out, code := run(t, "--config-dir", shippedConfig, "list-packs")
	if code != ExitOK {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	if strings.Contains(out, "icon-sets-example") {
		t.Fatalf("a private pack is listed with no private config dir:\n%s", out)
	}
	if !strings.Contains(out, "icon-sets-tabler") {
		t.Fatalf("list-packs listed nothing:\n%s", out)
	}
}

func TestAMissingPrivateConfigDirIsAUsageError(t *testing.T) {
	isolate(t)
	resetPrivateDir(t)
	out, code := run(t, "--config-dir", shippedConfig, "--private-config-dir", filepath.Join(t.TempDir(), "absent"), "list-packs")
	if code != ExitUsage {
		t.Fatalf("exit %d, want %d:\n%s", code, ExitUsage, out)
	}
}

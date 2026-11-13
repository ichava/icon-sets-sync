package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTree(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for rel, body := range files {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// validate finds the packs under --packages-dir, reports text, JSON and
// GitHub annotations, and exits 1 on an error.
func TestValidateCommand(t *testing.T) {
	isolate(t)
	t.Setenv("GITHUB_ACTIONS", "")
	dir := fixture(t, "", "")
	packs := t.TempDir()
	good := map[string]string{
		"composer.json":                    `{"name":"ichava/icon-sets-demo","type":"ichava-icon-set"}`,
		"LICENSE":                          "MIT",
		"resources/assets/svg/config.json": `{"schema_version":"1.0","package":{"name":"ichava/icon-sets-demo"}}`,
		"resources/assets/svg/licenses/demo/LICENSE": "MIT",
		"resources/assets/svg/files/a.svg":           `<svg xmlns="http://www.w3.org/2000/svg"/>`,
	}
	writeTree(t, filepath.Join(packs, "icon-sets-demo"), good)

	out, code := run(t, "--config-dir", dir, "--packages-dir", packs, "validate")
	if code != ExitOK || !strings.Contains(out, "ichava/icon-sets-demo: 1 sets, 1 SVGs") {
		t.Fatalf("good pack (%d):\n%s", code, out)
	}

	writeTree(t, filepath.Join(packs, "icon-sets-demo"), map[string]string{"resources/assets/svg/files/bad.svg": "<svg><g></svg>"})
	out, code = run(t, "--config-dir", dir, "--packages-dir", packs, "validate")
	if code != ExitFailure || !strings.Contains(out, "bad.svg does not parse") {
		t.Fatalf("bad svg (%d):\n%s", code, out)
	}

	summary := filepath.Join(t.TempDir(), "summary.md")
	t.Setenv("GITHUB_ACTIONS", "true")
	t.Setenv("GITHUB_STEP_SUMMARY", summary)
	out, _ = run(t, "--config-dir", dir, "--packages-dir", packs, "validate")
	if !strings.Contains(out, "::error file=resources/assets/svg/files/bad.svg,title=ichava/icon-sets-demo svg::") {
		t.Errorf("no annotation:\n%s", out)
	}
	if b, _ := os.ReadFile(summary); !strings.Contains(string(b), "| ichava/icon-sets-demo | 1 | 2 | 1 | 0 |") {
		t.Errorf("summary:\n%s", b)
	}

	t.Setenv("GITHUB_ACTIONS", "")
	out, _ = run(t, "--config-dir", dir, "--json", "--packages-dir", packs, "validate")
	var report struct {
		Errors int `json:"errors"`
		Packs  []struct {
			Name string `json:"name"`
		} `json:"packs"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil || report.Errors != 1 || report.Packs[0].Name != "ichava/icon-sets-demo" {
		t.Errorf("json %v:\n%s", err, out)
	}
}

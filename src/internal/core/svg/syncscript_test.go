package svg

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// syncScript is the cross-repository drift gate, at the repository root.
var syncScript = filepath.Join("..", "..", "..", "..", ".scripts", "sync-svg-policy.mjs")

// icon-browser's copy is the one the browser UI reads since react consolidation
// 3d; react-icon-browser is retired (3f, 2026-11-13).
const iconBrowserCopy = "icon-browser/resources/js/core/svg-policy.json"

func TestTheSyncScriptChecksIconBrowsersCopy(t *testing.T) {
	src, err := os.ReadFile(syncScript)
	if err != nil {
		t.Fatal(err)
	}
	s := string(src)
	if !strings.Contains(s, "'"+iconBrowserCopy+"'") {
		t.Errorf("%s does not list %s", syncScript, iconBrowserCopy)
	}
	if strings.Contains(s, "'react-icon-browser/") {
		t.Errorf("%s still lists a react-icon-browser path; that repository is retired", syncScript)
	}
}

// TestTheSyncScriptRunsAgainstIconBrowser runs the script over a workspace laid
// out like the real one: it passes when every copy matches core, and fails when
// icon-browser's copy drifts.
func TestTheSyncScriptRunsAgainstIconBrowser(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}
	script, err := filepath.Abs(syncScript)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	write := func(rel string, b []byte) {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("core/resources/security/svg-policy.json", policyJSON)
	write(iconBrowserCopy, policyJSON)
	write("icon-sets-sync/src/internal/core/svg/svg-policy.json", policyJSON)

	run := func() (string, error) {
		cmd := exec.Command(node, script)
		cmd.Env = append(os.Environ(), "ICHAVA_ROOT="+root)
		out, err := cmd.CombinedOutput()
		return string(out), err
	}
	out, err := run()
	if err != nil {
		t.Fatalf("matching copies failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, "ok       "+iconBrowserCopy) {
		t.Fatalf("the run did not check %s:\n%s", iconBrowserCopy, out)
	}

	write(iconBrowserCopy, append([]byte("{}"), policyJSON...))
	out, err = run()
	if err == nil || !strings.Contains(out, "DRIFT    "+iconBrowserCopy) {
		t.Fatalf("a drifted icon-browser copy passed: %v\n%s", err, out)
	}
}

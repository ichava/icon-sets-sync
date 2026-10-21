package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ichava/maintainer-toolkit/src/internal/core/checker"
	"github.com/ichava/maintainer-toolkit/src/internal/core/config"
	"github.com/ichava/maintainer-toolkit/src/internal/core/pipeline"
	"github.com/ichava/maintainer-toolkit/src/internal/core/recipes"
)

// isolate keeps a test away from the developer's real global config and from
// any ICHAVA_* variable in the environment running the tests.
func isolate(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	for _, kv := range os.Environ() {
		if name, _, _ := strings.Cut(kv, "="); strings.HasPrefix(name, "ICHAVA_") {
			t.Setenv(name, "")
		}
	}
}

// run executes the command tree and returns stdout and the exit code.
func run(t *testing.T, args ...string) (string, int) {
	t.Helper()
	root := NewRootCommand()
	var out, errOut bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errOut)
	root.SetArgs(args)
	root.SetContext(context.Background())

	code := ExitOK
	if err := root.Execute(); err != nil {
		var exit *ExitError
		if asExitError(err, &exit) {
			code = exit.Code
			if exit.Err != nil {
				errOut.WriteString(exit.Err.Error())
			}
		} else {
			code = ExitFailure
			errOut.WriteString(err.Error())
		}
	}
	return out.String() + errOut.String(), code
}

// fixture writes a config dir with one pack, and the pack repo it points at.
func fixture(t *testing.T, packSettings string, sinks string) string {
	t.Helper()
	dir := t.TempDir()
	packRoot := filepath.Join(dir, "icon-sets-demo")
	if err := os.MkdirAll(filepath.Join(packRoot, "resources", "assets", "svg"), 0o755); err != nil {
		t.Fatal(err)
	}
	if sinks == "" {
		sinks = `[{"type": "filesystem", "root": "{pack_root}/files"}, {"type": "git-branch"}]`
	}
	body := `{
  "name": "icon-sets-demo",
  "pack": "ichava/icon-sets-demo",
  "pack_root": "` + packRoot + `",
  "current_version": "1.0.0",
  "version_check_url": "https://registry.npmjs.org/demo/latest",
  "source": {"type": "npm", "package": "demo"},
  "sinks": ` + sinks
	if packSettings != "" {
		body += `,
  "settings": ` + packSettings
	}
	body += "\n}\n"
	write(t, filepath.Join(dir, "icon-sets-demo.json"), body)
	write(t, filepath.Join(dir, "packs.json"), `{"packs": ["icon-sets-demo"]}`)
	return dir
}

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

type printed struct {
	Settings map[string]any    `json:"settings"`
	Origins  map[string]string `json:"origins"`
	Recipe   string            `json:"recipe"`
}

func printConfig(t *testing.T, args ...string) printed {
	t.Helper()
	out, code := run(t, append([]string{"config", "print", "--origins"}, args...)...)
	if code != ExitOK {
		t.Fatalf("config print exited %d:\n%s", code, out)
	}
	var p printed
	if err := json.Unmarshal([]byte(out), &p); err != nil {
		t.Fatalf("config print is not JSON: %v\n%s", err, out)
	}
	return p
}

func gitBase(p printed) any { return p.Settings["git"].(map[string]any)["base"] }

// TestPrecedenceEndToEnd is the precedence contract through the real command
// line: one row per level, each adding a value above the last.
func TestPrecedenceEndToEnd(t *testing.T) {
	tests := []struct {
		name       string
		packLayer  bool
		globalFile bool
		env        bool
		flag       bool
		want       string
		origin     string
	}{
		{"defaults", false, false, false, false, "main", "default"},
		{"pack beats defaults", true, false, false, false, "from-pack", "pack"},
		{"global beats pack", true, true, false, false, "from-global", "global"},
		{"env beats global", true, true, true, false, "from-env", "env"},
		{"flag beats env", true, true, true, true, "from-flag", "flag"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isolate(t)
			packSettings := ""
			if tt.packLayer {
				packSettings = `{"git": {"base": "from-pack"}}`
			}
			dir := fixture(t, packSettings, "")
			args := []string{"--config-dir", dir, "--pack", "icon-sets-demo"}

			if tt.globalFile {
				// Found by the XDG lookup, not passed explicitly, so the
				// discovery is exercised too.
				xdg := t.TempDir()
				t.Setenv("XDG_CONFIG_HOME", xdg)
				if err := os.MkdirAll(filepath.Join(xdg, "icon-sets-sync"), 0o755); err != nil {
					t.Fatal(err)
				}
				write(t, filepath.Join(xdg, "icon-sets-sync", "config.yaml"), "git:\n  base: from-global\n")
			}
			if tt.env {
				t.Setenv("ICHAVA_GIT_BASE", "from-env")
			}
			if tt.flag {
				args = append(args, "--set", "git.base=from-flag")
			}

			p := printConfig(t, args...)
			if got := gitBase(p); got != tt.want {
				t.Errorf("git.base = %v, want %s", got, tt.want)
			}
			if got := p.Origins["git.base"]; got != tt.origin {
				t.Errorf("origin = %s, want %s", got, tt.origin)
			}
		})
	}
}

func TestDedicatedFlagsAreTheFlagLayer(t *testing.T) {
	tests := []struct {
		name   string
		env    map[string]string
		args   []string
		key    string
		want   any
		origin string
	}{
		{"-v is debug", nil, []string{"-v"}, "log_level", "debug", "flag"},
		{"-q is error", nil, []string{"-q"}, "log_level", "error", "flag"},
		{"ICHAVA_LOG_LEVEL without a flag", map[string]string{"ICHAVA_LOG_LEVEL": "warn"}, nil, "log_level", "warn", "env"},
		{"-v beats ICHAVA_LOG_LEVEL", map[string]string{"ICHAVA_LOG_LEVEL": "warn"}, []string{"-v"}, "log_level", "debug", "flag"},
		{"--config-dir beats ICHAVA_CONFIG_DIR", map[string]string{"ICHAVA_CONFIG_DIR": "/nope"}, nil, "config_dir", "", "flag"},
		{"ICHAVA_DRY_RUN is read", map[string]string{"ICHAVA_DRY_RUN": "true"}, nil, "dry_run", true, "env"},
		{"an unset flag does not mask the env", map[string]string{"ICHAVA_FORCE": "1"}, nil, "force", true, "env"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isolate(t)
			dir := fixture(t, "", "")
			for k, v := range tt.env {
				t.Setenv(k, v)
			}
			args := append([]string{"--config-dir", dir}, tt.args...)
			p := printConfig(t, args...)

			want := tt.want
			if tt.key == "config_dir" {
				want = dir
			}
			got := lookup(p.Settings, tt.key)
			if got != want {
				t.Errorf("%s = %v, want %v", tt.key, got, want)
			}
			if p.Origins[tt.key] != tt.origin {
				t.Errorf("origin = %s, want %s", p.Origins[tt.key], tt.origin)
			}
		})
	}
}

func lookup(tree map[string]any, dotted string) any {
	var cur any = tree
	for _, part := range strings.Split(dotted, ".") {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = m[part]
	}
	return cur
}

func TestUsageErrors(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{"-v and -q", []string{"-v", "-q", "version"}},
		{"--dry-run and --commit", []string{"sync", "--pack", "x", "--dry-run", "--commit"}},
		{"sync with no target", []string{"sync"}},
		{"sync with both targets", []string{"sync", "--pack", "x", "--all"}},
		{"unknown --set key", []string{"--set", "nope=1", "version"}},
		{"bad --set value", []string{"--set", "concurrency=0", "version"}},
		{"bad --concurrency", []string{"sync-all", "--concurrency", "0"}},
		{"unknown flag", []string{"--bogus"}},
		{"bad print format", []string{"config", "print", "--format", "toml"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isolate(t)
			out, code := run(t, tt.args...)
			if code != ExitUsage {
				t.Errorf("exit %d, want %d:\n%s", code, ExitUsage, out)
			}
		})
	}
}

func TestABadEnvValueIsAUsageError(t *testing.T) {
	isolate(t)
	t.Setenv("ICHAVA_CONCURRENCY", "lots")
	out, code := run(t, "version")
	if code != ExitUsage || !strings.Contains(out, "ICHAVA_CONCURRENCY") {
		t.Fatalf("exit %d:\n%s", code, out)
	}
}

func TestABadGlobalConfigFailsAndNamesTheFile(t *testing.T) {
	isolate(t)
	path := filepath.Join(t.TempDir(), "g.yaml")
	write(t, path, "dry_rnu: true\n")
	out, code := run(t, "--global-config", path, "version")
	if code != ExitFailure || !strings.Contains(out, path) || !strings.Contains(out, "dry_rnu") {
		t.Fatalf("exit %d:\n%s", code, out)
	}
}

func TestVersion(t *testing.T) {
	isolate(t)
	out, code := run(t, "version")
	if code != ExitOK || !strings.HasPrefix(out, "icon-sets-sync ") {
		t.Fatalf("exit %d: %q", code, out)
	}
}

type fakeChecker map[string]checker.Result

func (f fakeChecker) CheckPack(_ context.Context, p *config.PackConfig) checker.Result {
	return f[p.Name]
}

func withSeams(t *testing.T, chk fakeChecker, ran *[]string) {
	t.Helper()
	syncChecker = chk
	syncRun = func(_ context.Context, p *config.PackConfig, v string, o recipes.Options) (pipeline.Result, error) {
		mode := "commit"
		if o.DryRun {
			mode = "dry-run"
		}
		*ran = append(*ran, p.Name+"@"+v+":"+mode)
		return pipeline.Result{PipelineName: p.Name, Success: true}, nil
	}
	t.Cleanup(func() { syncChecker, syncRun = nil, nil })
}

func TestSyncCommand(t *testing.T) {
	stale := checker.Result{Current: "1.0.0", Latest: "2.0.0", Stale: true}
	fresh := checker.Result{Current: "2.0.0", Latest: "2.0.0"}
	dead := checker.Result{Current: "1.0.0", Reason: "unreachable: 404"}

	tests := []struct {
		name     string
		check    checker.Result
		sinks    string
		args     []string
		env      map[string]string
		wantCode int
		wantRan  string
		wantOut  string
	}{
		{"a stale pack syncs", stale, "", []string{"sync", "--pack", "icon-sets-demo"}, nil,
			ExitOK, "icon-sets-demo@2.0.0:commit", "done"},
		{"a fresh pack is skipped", fresh, "", []string{"sync", "--pack", "icon-sets-demo"}, nil,
			ExitOK, "", "up-to-date"},
		{"--force refreshes a fresh pack", fresh, "", []string{"sync", "--pack", "icon-sets-demo", "--force"}, nil,
			ExitOK, "icon-sets-demo@2.0.0:commit", "done"},
		{"an unresolvable upstream fails loudly", dead, "", []string{"sync", "--pack", "icon-sets-demo"}, nil,
			ExitFailure, "", "cannot resolve"},
		{"--dry-run reaches the recipe", stale, "", []string{"sync", "--pack", "icon-sets-demo", "--dry-run"}, nil,
			ExitOK, "icon-sets-demo@2.0.0:dry-run", "dry run"},
		{"ICHAVA_DRY_RUN reaches the recipe", stale, "", []string{"sync", "--pack", "icon-sets-demo"},
			map[string]string{"ICHAVA_DRY_RUN": "1"}, ExitOK, "icon-sets-demo@2.0.0:dry-run", ""},
		{"--commit overrides ICHAVA_DRY_RUN", stale, "", []string{"sync", "--pack", "icon-sets-demo", "--commit"},
			map[string]string{"ICHAVA_DRY_RUN": "1"}, ExitOK, "icon-sets-demo@2.0.0:commit", ""},
		{"empty sinks fail outside a dry run", stale, "[]", []string{"sync", "--pack", "icon-sets-demo"}, nil,
			ExitFailure, "", "declares no sinks"},
		{"empty sinks are fine in a dry run", stale, "[]", []string{"sync", "--pack", "icon-sets-demo", "--dry-run"}, nil,
			ExitOK, "icon-sets-demo@2.0.0:dry-run", ""},
		{"sync --all", stale, "", []string{"sync", "--all"}, nil,
			ExitOK, "icon-sets-demo@2.0.0:commit", ""},
		{"sync-all", stale, "", []string{"sync-all", "--concurrency", "2"}, nil,
			ExitOK, "icon-sets-demo@2.0.0:commit", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isolate(t)
			for k, v := range tt.env {
				t.Setenv(k, v)
			}
			dir := fixture(t, "", tt.sinks)
			var ran []string
			withSeams(t, fakeChecker{"icon-sets-demo": tt.check}, &ran)

			out, code := run(t, append([]string{"--config-dir", dir}, tt.args...)...)
			if code != tt.wantCode {
				t.Errorf("exit %d, want %d:\n%s", code, tt.wantCode, out)
			}
			if got := strings.Join(ran, ","); got != tt.wantRan {
				t.Errorf("ran %q, want %q", got, tt.wantRan)
			}
			if tt.wantOut != "" && !strings.Contains(out, tt.wantOut) {
				t.Errorf("output lacks %q:\n%s", tt.wantOut, out)
			}
		})
	}
}

func TestSyncAllConcurrencyReachesTheRun(t *testing.T) {
	isolate(t)
	dir := fixture(t, "", "")
	var ran []string
	withSeams(t, fakeChecker{"icon-sets-demo": {Latest: "2", Stale: true}}, &ran)

	if out, code := run(t, "--config-dir", dir, "sync-all", "--concurrency", "3"); code != ExitOK {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	if got := state.run.Int("concurrency"); got != 3 {
		t.Errorf("concurrency = %d, want 3", got)
	}
}

func TestConfigValidateTheShippedConfigs(t *testing.T) {
	isolate(t)
	dir := filepath.Join("..", "..", "..", "config")
	out, code := run(t, "--config-dir", dir, "config", "validate")
	if code != ExitOK {
		t.Fatalf("the shipped configs do not validate (exit %d):\n%s", code, out)
	}
	// Assert the size of what was inspected: a registry that matched nothing
	// would otherwise validate cleanly.
	for _, slug := range []string{"icon-sets-tabler", "icon-sets-flag", "icon-sets-bundled", "icon-sets-emoji"} {
		if !strings.Contains(out, slug) {
			t.Errorf("validate did not report %s:\n%s", slug, out)
		}
	}
}

func TestConfigValidateReportsABadFile(t *testing.T) {
	isolate(t)
	dir := fixture(t, `{"concurrency": 2}`, "")
	out, code := run(t, "--config-dir", dir, "config", "validate")
	if code != ExitFailure || !strings.Contains(out, "/settings") {
		t.Fatalf("exit %d:\n%s", code, out)
	}

	good := filepath.Join(fixture(t, "", ""), "icon-sets-demo.json")
	if out, code := run(t, "config", "validate", good); code != ExitOK {
		t.Fatalf("a valid file argument failed (exit %d):\n%s", code, out)
	}
}

func TestConfigPrintYAMLAndPack(t *testing.T) {
	isolate(t)
	dir := fixture(t, "", "")
	out, code := run(t, "--config-dir", dir, "config", "print", "--pack", "icon-sets-demo", "--format", "yaml")
	if code != ExitOK {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	for _, want := range []string{"settings:", "pack:", "name: icon-sets-demo", "recipe: generic", "base: main"} {
		if !strings.Contains(out, want) {
			t.Errorf("yaml output lacks %q:\n%s", want, out)
		}
	}
}

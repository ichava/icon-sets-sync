package settings

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func env(m map[string]string) func(string) (string, bool) {
	return func(k string) (string, bool) {
		v, ok := m[k]
		return v, ok
	}
}

// TestPrecedence pins each level against the one below it. One row per level:
// a key set at that level, and at every level beneath it with a different
// value, must resolve to that level's value and report that level as origin.
func TestPrecedence(t *testing.T) {
	pack := Values{"git.base": "from-pack"}
	global := Values{"git.base": "from-global"}
	envv := Values{"git.base": "from-env"}
	flags := Values{"git.base": "from-flag"}

	tests := []struct {
		name  string
		stack Stack
		pack  Values
		want  string
		layer Layer
	}{
		{"defaults when nothing is set", Stack{}, nil, DefaultBase, LayerDefault},
		{"pack beats defaults", Stack{}, pack, "from-pack", LayerPack},
		{"global beats pack", Stack{Global: global}, pack, "from-global", LayerGlobal},
		{"env beats global", Stack{Global: global, Env: envv}, pack, "from-env", LayerEnv},
		{"flags beat env", Stack{Global: global, Env: envv, Flags: flags}, pack, "from-flag", LayerFlag},
		{"flags beat a lower layer that is absent", Stack{Flags: flags}, nil, "from-flag", LayerFlag},
		{"global still beats pack with no env", Stack{Global: global}, pack, "from-global", LayerGlobal},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := tt.stack.Resolve(tt.pack)
			if got := r.String("git.base"); got != tt.want {
				t.Errorf("git.base = %q, want %q", got, tt.want)
			}
			if got := r.Origin("git.base"); got != tt.layer {
				t.Errorf("origin = %q, want %q", got, tt.layer)
			}
		})
	}
}

// A higher layer replaces only the keys it sets; the rest fall through.
func TestLayersMergeKeyByKey(t *testing.T) {
	r := Stack{
		Global: Values{"concurrency": 3},
		Flags:  Values{"dry_run": true},
	}.Resolve(Values{"force": true})

	s := r.Settings()
	if s.Concurrency != 3 || !s.DryRun || !s.Force || s.Git.Base != DefaultBase {
		t.Fatalf("merge lost a layer: %+v", s)
	}
	if r.Origin("force") != LayerPack || r.Origin("concurrency") != LayerGlobal ||
		r.Origin("dry_run") != LayerFlag || r.Origin("git.base") != LayerDefault {
		t.Fatalf("origins = %v", r.Origins())
	}
}

func TestFromAbove(t *testing.T) {
	tests := []struct {
		layer Layer
		want  bool
	}{
		{LayerDefault, false}, {LayerPack, false}, {LayerGlobal, true}, {LayerEnv, true}, {LayerFlag, true},
	}
	for _, tt := range tests {
		t.Run(string(tt.layer), func(t *testing.T) {
			r := Resolve(LayerValues{tt.layer, Values{"git.branch": "x"}})
			if got := r.FromAbove("git.branch"); got != tt.want {
				t.Errorf("FromAbove = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseString(t *testing.T) {
	tests := []struct {
		key     string
		raw     string
		want    any
		wantErr string
	}{
		{"dry_run", "true", true, ""},
		{"dry_run", "YES", true, ""},
		{"dry_run", "0", false, ""},
		{"dry_run", "maybe", nil, "not a boolean"},
		{"concurrency", "4", 4, ""},
		{"concurrency", "0", nil, "at least 1"},
		{"concurrency", "four", nil, "not an integer"},
		{"log_level", "DEBUG", "debug", ""},
		{"log_level", "WARNING", "warn", ""},
		{"log_level", "loud", nil, "not one of"},
		{"git.author", "Bot <bot@example.com>", "Bot <bot@example.com>", ""},
		{"git.author", "no-email", nil, "does not match"},
		{"git.base", "develop", "develop", ""},
	}
	for _, tt := range tests {
		t.Run(tt.key+"="+tt.raw, func(t *testing.T) {
			key, _ := Lookup(tt.key)
			got, err := key.ParseString(tt.raw)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Errorf("got %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestFromTree(t *testing.T) {
	tests := []struct {
		name    string
		tree    map[string]any
		layer   Layer
		want    Values
		wantErr string
	}{
		{
			name:  "nested keys flatten",
			tree:  map[string]any{"git": map[string]any{"base": "trunk"}, "dry_run": true},
			layer: LayerGlobal,
			want:  Values{"git.base": "trunk", "dry_run": true},
		},
		{
			name:  "JSON numbers become integers",
			tree:  map[string]any{"concurrency": float64(2)},
			layer: LayerGlobal,
			want:  Values{"concurrency": 2},
		},
		{
			name:  "underscore notes are skipped",
			tree:  map[string]any{"_note": "hi", "force": true},
			layer: LayerGlobal,
			want:  Values{"force": true},
		},
		{
			name:    "an unknown key is an error, not a no-op",
			tree:    map[string]any{"dry_rnu": true},
			layer:   LayerGlobal,
			wantErr: `unknown setting "dry_rnu"`,
		},
		{
			name:    "an unquoted YAML version is refused",
			tree:    map[string]any{"emoji": map[string]any{"unicode_version": float64(16)}},
			layer:   LayerGlobal,
			wantErr: "quote it in YAML",
		},
		{
			name:    "a run-wide key cannot be set per pack",
			tree:    map[string]any{"concurrency": float64(4)},
			layer:   LayerPack,
			wantErr: "run-wide",
		},
		{
			name:    "a fractional concurrency is refused",
			tree:    map[string]any{"concurrency": 1.5},
			layer:   LayerGlobal,
			wantErr: "want an integer",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := FromTree(tt.tree, tt.layer)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
			for k, v := range tt.want {
				if got[k] != v {
					t.Errorf("%s = %#v, want %#v", k, got[k], v)
				}
			}
		})
	}
}

func TestFromEnv(t *testing.T) {
	tests := []struct {
		name    string
		env     map[string]string
		key     string
		want    any
		wantErr string
	}{
		{"current name", map[string]string{"ICHAVA_DRY_RUN": "1"}, "dry_run", true, ""},
		{"nested key", map[string]string{"ICHAVA_GIT_BASE": "trunk"}, "git.base", "trunk", ""},
		{"legacy name still works", map[string]string{"ICHAVA_DEV_CONFIG_DIR": "/app/config"}, "config_dir", "/app/config", ""},
		{"current beats legacy", map[string]string{"ICHAVA_CONFIG_DIR": "/new", "ICHAVA_DEV_CONFIG_DIR": "/old"}, "config_dir", "/new", ""},
		{"legacy log level in Python's casing", map[string]string{"ICHAVA_DEV_LOG_LEVEL": "INFO"}, "log_level", "info", ""},
		{"empty is unset", map[string]string{"ICHAVA_FORCE": ""}, "force", nil, ""},
		{"bad value names the variable", map[string]string{"ICHAVA_CONCURRENCY": "x"}, "", nil, "ICHAVA_CONCURRENCY"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := FromEnv(env(tt.env))
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got[tt.key] != tt.want {
				t.Errorf("%s = %#v, want %#v", tt.key, got[tt.key], tt.want)
			}
		})
	}
}

func TestEveryKeyHasAnIchavaEnvVar(t *testing.T) {
	for _, k := range Keys {
		want := "ICHAVA_" + strings.ToUpper(strings.ReplaceAll(k.Name, ".", "_"))
		if k.Env != want {
			t.Errorf("%s: env %q, want %q", k.Name, k.Env, want)
		}
		if _, err := k.coerce(k.Default); err != nil {
			t.Errorf("%s: default %#v does not satisfy its own type: %v", k.Name, k.Default, err)
		}
	}
}

func TestFromAssignments(t *testing.T) {
	got, err := FromAssignments([]string{"git.base=trunk", "concurrency=3"})
	if err != nil {
		t.Fatal(err)
	}
	if got["git.base"] != "trunk" || got["concurrency"] != 3 {
		t.Fatalf("got %v", got)
	}
	for _, bad := range []string{"novalue", "nope=1", "force=sometimes"} {
		if _, err := FromAssignments([]string{bad}); err == nil {
			t.Errorf("%q: want an error", bad)
		}
	}
}

func TestTreeNestsDottedNames(t *testing.T) {
	tree := Resolve().Tree()
	git, ok := tree["git"].(map[string]any)
	if !ok || git["base"] != DefaultBase {
		t.Fatalf("tree = %v", tree)
	}
}

func TestGlobalConfigPath(t *testing.T) {
	dir := t.TempDir()
	appDir := filepath.Join(dir, AppDir)
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// None present: optional, so no path and no error.
	got, err := GlobalConfigPath(env(map[string]string{"XDG_CONFIG_HOME": dir}))
	if err != nil || got != "" {
		t.Fatalf("empty: got %q, %v", got, err)
	}

	yamlPath := filepath.Join(appDir, "config.yaml")
	if err := os.WriteFile(yamlPath, []byte("concurrency: 2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err = GlobalConfigPath(env(map[string]string{"XDG_CONFIG_HOME": dir}))
	if err != nil || got != yamlPath {
		t.Fatalf("yaml: got %q, %v", got, err)
	}

	// HOME/.config is the fallback when XDG_CONFIG_HOME is unset.
	home := t.TempDir()
	homeApp := filepath.Join(home, ".config", AppDir)
	if err := os.MkdirAll(homeApp, 0o755); err != nil {
		t.Fatal(err)
	}
	jsonPath := filepath.Join(homeApp, "config.json")
	if err := os.WriteFile(jsonPath, []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err = GlobalConfigPath(env(map[string]string{"HOME": home}))
	if err != nil || got != jsonPath {
		t.Fatalf("home: got %q, %v", got, err)
	}

	// Both formats at once is ambiguous and refused.
	if err := os.WriteFile(filepath.Join(appDir, "config.json"), []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := GlobalConfigPath(env(map[string]string{"XDG_CONFIG_HOME": dir})); err == nil {
		t.Fatal("two global configs: want an error")
	}

	// The explicit variable wins outright.
	got, err = GlobalConfigPath(env(map[string]string{GlobalConfigEnv: "/x/y.yaml", "XDG_CONFIG_HOME": dir}))
	if err != nil || got != "/x/y.yaml" {
		t.Fatalf("explicit: got %q, %v", got, err)
	}
}

func TestLoadGlobal(t *testing.T) {
	dir := t.TempDir()
	tests := []struct {
		file, body string
		wantErr    string
		check      func(t *testing.T, v Values)
	}{
		{"g.yaml", "git:\n  base: trunk\nconcurrency: 2\n", "", func(t *testing.T, v Values) {
			if v["git.base"] != "trunk" || v["concurrency"] != 2 {
				t.Errorf("got %v", v)
			}
		}},
		{"g.json", `{"dry_run": true}`, "", func(t *testing.T, v Values) {
			if v["dry_run"] != true {
				t.Errorf("got %v", v)
			}
		}},
		{"bad.yaml", "nope: 1\n", "unknown setting", nil},
		{"list.json", `[1,2]`, "object", nil},
	}
	for _, tt := range tests {
		t.Run(tt.file, func(t *testing.T) {
			path := filepath.Join(dir, tt.file)
			if err := os.WriteFile(path, []byte(tt.body), 0o644); err != nil {
				t.Fatal(err)
			}
			v, err := LoadGlobal(path)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			tt.check(t, v)
		})
	}

	if v, err := LoadGlobal(""); err != nil || len(v) != 0 {
		t.Fatalf("no path: %v, %v", v, err)
	}
}

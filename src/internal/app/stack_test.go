package app

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/ichava/icon-sets-sync/src/internal/core/settings"
)

func env(kv map[string]string) func(string) (string, bool) {
	return func(k string) (string, bool) {
		v, ok := kv[k]
		return v, ok
	}
}

func TestBuildStackLayersInPrecedenceOrder(t *testing.T) {
	dir := t.TempDir()
	global := filepath.Join(dir, "config.json")
	if err := os.WriteFile(global, []byte(`{"git": {"base": "from-global", "branch": "from-global"}, "concurrency": 2}`), 0o644); err != nil {
		t.Fatal(err)
	}

	stack, path, err := BuildStack(StackInput{
		GlobalPath:  global,
		Lookup:      env(map[string]string{"ICHAVA_GIT_BASE": "from-env", "HOME": dir}),
		Assignments: []string{"git.base=from-set", "concurrency=5"},
		Overlay: func(v settings.Values) error {
			// A dedicated flag runs after --set, so it wins.
			return v.Set("concurrency", 7)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if path != global {
		t.Errorf("global path = %q, want %q", path, global)
	}
	r := stack.Resolve(nil)
	for key, want := range map[string]any{
		"git.branch": "from-global", "git.base": "from-set", "concurrency": 7,
	} {
		var got any
		switch want.(type) {
		case int:
			got = r.Int(key)
		default:
			got = r.String(key)
		}
		if got != want {
			t.Errorf("%s = %v, want %v", key, got, want)
		}
	}
	if o := r.Origin("git.branch"); o != settings.LayerGlobal {
		t.Errorf("git.branch origin = %s, want global", o)
	}
	if o := r.Origin("git.base"); o != settings.LayerFlag {
		t.Errorf("git.base origin = %s, want flag", o)
	}
}

func TestBuildStackDiscoversTheGlobalConfig(t *testing.T) {
	xdg := t.TempDir()
	want := filepath.Join(xdg, settings.AppDir, "config.json")
	if err := os.MkdirAll(filepath.Dir(want), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(want, []byte(`{"git": {"base": "develop"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	stack, path, err := BuildStack(StackInput{Lookup: env(map[string]string{"XDG_CONFIG_HOME": xdg})})
	if err != nil {
		t.Fatal(err)
	}
	if path != want || stack.Resolve(nil).String("git.base") != "develop" {
		t.Fatalf("path %q, git.base %q", path, stack.Resolve(nil).String("git.base"))
	}
}

func TestBuildStackSeparatesUsageFromFailure(t *testing.T) {
	home := env(map[string]string{"HOME": t.TempDir()})
	cases := map[string]struct {
		in    StackInput
		usage bool
	}{
		"bad env value": {StackInput{Lookup: env(map[string]string{"ICHAVA_CONCURRENCY": "lots", "HOME": "/nonexistent"})}, true},
		"bad --set":     {StackInput{Lookup: home, Assignments: []string{"nope=1"}}, true},
		"bad overlay": {StackInput{Lookup: home, Overlay: func(settings.Values) error {
			return errors.New("--concurrency: too low")
		}}, true},
		"missing global file": {StackInput{Lookup: home, GlobalPath: filepath.Join(t.TempDir(), "absent.json")}, false},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			_, _, err := BuildStack(c.in)
			if err == nil {
				t.Fatal("no error")
			}
			if IsUsage(err) != c.usage {
				t.Errorf("IsUsage = %v, want %v (%v)", IsUsage(err), c.usage, err)
			}
		})
	}
}

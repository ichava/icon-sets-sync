package web

import (
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ichava/icon-sets-sync/src/internal/core/settings"
)

func key(t *testing.T, name string) settings.Key {
	t.Helper()
	k, ok := settings.Lookup(name)
	if !ok {
		t.Fatalf("no setting %s", name)
	}
	return k
}

// Every value the page accepts is one the loader accepts, and the form adds
// checks for values the loader takes as any string.
func TestParseSettingRefusesWhatARunWouldTripOn(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "npm")
	if err := os.WriteFile(exe, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	plain := filepath.Join(dir, "plain")
	if err := os.WriteFile(plain, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	ok := []struct {
		name string
		raw  []string
		want any
	}{
		{"concurrency", []string{" 4 "}, 4},
		{"page_size", []string{"50"}, 50},
		{"log_level", []string{"WARNING"}, "warn"},
		{"dry_run", []string{"false", "true"}, true}, // a switch that is on
		{"dry_run", []string{"false"}, false},        // and off
		{"allowed_hosts", []string{"Example.org, cdn.example.com"}, "example.org,cdn.example.com"},
		{"git.branch", []string{"chore/sync-{version}"}, "chore/sync-{version}"},
		{"git.author", []string{"Bot <bot@example.org>"}, "Bot <bot@example.org>"},
		{"emoji.unicode_version", []string{"16.0"}, "16.0"},
		{"workspace", []string{dir}, dir},
		{"npm_path", []string{exe}, exe},
		{"git.commit_body", []string{"line one\n\nline two"}, "line one\n\nline two"},
	}
	for _, c := range ok {
		got, _, err := parseSetting(key(t, c.name), c.raw)
		if err != nil || !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s %q = %#v, %v; want %#v", c.name, c.raw, got, err, c.want)
		}
	}
	bad := []struct {
		name, raw, says string
	}{
		{"concurrency", "0", "at least 1"},
		{"concurrency", "2.5", "not a whole number"},
		{"concurrency", "", "needs a number"},
		{"page_size", "4", "at least 5"},
		{"page_size", "501", "at most 500"},
		{"log_level", "loud", "not one of"},
		{"allowed_hosts", "https://example.org", "not a host name"},
		{"allowed_hosts", "example", "not a host name"},
		{"git.base", "main branch", "not a valid git branch"},
		{"git.base", "-main", "not a valid git branch"},
		{"git.branch", "a..b", "not a valid git branch"},
		{"git.author", "Bot", "does not match"},
		{"emoji.unicode_version", "16", "not a Unicode emoji release"},
		{"workspace", "relative/dir", "not an absolute path"},
		{"workspace", filepath.Join(dir, "gone"), "does not exist"},
		{"workspace", plain, "not a directory"},
		{"npm_path", plain, "not executable"},
		{"npm_path", dir, "is a directory"},
		{"git.base", "two\nlines", "one line"},
		{"git.base", "   ", "is empty"},
		{"git.base", strings.Repeat("a", maxSettingLen+1), "longer than"},
		{"dry_run", "maybe", "not a boolean"},
	}
	for _, c := range bad {
		_, _, err := parseSetting(key(t, c.name), []string{c.raw})
		if err == nil || !strings.Contains(err.Error(), c.says) {
			t.Errorf("%s %q: err = %v, want one saying %q", c.name, c.raw, err, c.says)
		}
	}
}

func TestSetTreeKeyNestsByDots(t *testing.T) {
	tree := map[string]any{"git.base": "flat", "git": map[string]any{"author": "A <a@b.c>"}, "concurrency": 2.0}
	setTreeKey(tree, "git.base", "develop")
	want := map[string]any{"git": map[string]any{"author": "A <a@b.c>", "base": "develop"}, "concurrency": 2.0}
	if !reflect.DeepEqual(tree, want) {
		t.Fatalf("set: %#v", tree)
	}
	setTreeKey(tree, "git.base", nil)
	setTreeKey(tree, "git.author", nil)
	if !reflect.DeepEqual(tree, map[string]any{"concurrency": 2.0}) {
		t.Fatalf("an emptied table was left behind: %#v", tree)
	}
	setTreeKey(tree, "emoji.unicode_version", nil) // removing what is not there
	if len(tree) != 1 {
		t.Fatalf("removing a missing key changed the tree: %#v", tree)
	}
}

func readGlobal(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var tree map[string]any
	if err := json.Unmarshal(data, &tree); err != nil {
		t.Fatalf("%s: %v", data, err)
	}
	return tree
}

// Saving a value writes it into the global config, nested, keeping every
// other key; Reset removes it again.
func TestSettingSavesAndResetsInTheGlobalConfig(t *testing.T) {
	s, eng := newTestServer(t)
	global := filepath.Join(eng.cfg.GlobalHome, settings.AppDir, "config.json")
	if err := os.MkdirAll(filepath.Dir(global), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(global, []byte(`{"concurrency": 2}`), 0o644); err != nil {
		t.Fatal(err)
	}
	c := signIn(t, s)
	resp := c.post("/settings/value/git.base", url.Values{"value": {"develop"}, "action": {"save"}})
	if resp.Code != 303 || !strings.Contains(resp.Header().Get("Location"), "saved=git.base#setting-git-base") {
		t.Fatalf("save = %d %s:\n%s", resp.Code, resp.Header().Get("Location"), resp.Body.String())
	}
	tree := readGlobal(t, global)
	if !reflect.DeepEqual(tree["git"], map[string]any{"base": "develop"}) || tree["concurrency"] != 2.0 {
		t.Fatalf("global config = %v", tree)
	}
	resp = c.post("/settings/value/git.base", url.Values{"action": {"reset"}})
	if resp.Code != 303 || !strings.Contains(resp.Header().Get("Location"), "reset=git.base") {
		t.Fatalf("reset = %d %s", resp.Code, resp.Header().Get("Location"))
	}
	if tree := readGlobal(t, global); tree["git"] != nil || tree["concurrency"] != 2.0 {
		t.Fatalf("after reset: %v", tree)
	}
}

// A refused save writes nothing, says why beside the field, and keeps what
// was typed.
func TestSettingRefusedSaveKeepsTheInput(t *testing.T) {
	s, eng := newTestServer(t)
	global := filepath.Join(eng.cfg.GlobalHome, settings.AppDir, "config.json")
	c := signIn(t, s)
	resp := c.post("/settings/value/page_size", url.Values{"value": {"9000"}, "action": {"save"}})
	if resp.Code != 422 {
		t.Fatalf("save = %d", resp.Code)
	}
	body := resp.Body.String()
	for _, want := range []string{"page_size must be at most 500", `value="9000"`, "is-invalid"} {
		if !strings.Contains(body, want) {
			t.Errorf("refused page lacks %q", want)
		}
	}
	if _, err := os.Stat(global); err == nil {
		t.Error("a refused save wrote the global config")
	}
	if got := c.post("/settings/value/no_such_key", url.Values{"value": {"1"}}).Code; got != 404 {
		t.Errorf("unknown setting = %d, want 404", got)
	}
	if got := c.post("/settings/value/git.base", url.Values{"value": {"x"}, "action": {"drop"}}).Code; got != 422 {
		t.Errorf("unknown action = %d, want 422", got)
	}
}

// The check endpoint answers what a save would, and writes nothing; a
// read-only server keeps it and has no save route.
func TestSettingCheckAnswersJSON(t *testing.T) {
	s, _ := newTestServer(t, func(o *Options) { o.ReadOnly = true })
	c := signIn(t, s)
	resp := c.postJSON("/settings/value/concurrency/check", url.Values{"value": {"0"}})
	var out struct {
		OK    bool
		Error string
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &out); err != nil || out.OK || !strings.Contains(out.Error, "at least 1") {
		t.Fatalf("check = %d %s", resp.Code, resp.Body.String())
	}
	resp = c.postJSON("/settings/value/concurrency/check", url.Values{"value": {"3"}})
	if !strings.Contains(resp.Body.String(), `"ok":true`) {
		t.Fatalf("check of a good value = %s", resp.Body.String())
	}
	if got := c.post("/settings/value/concurrency", url.Values{"value": {"3"}}).Code; got != 405 {
		t.Errorf("read-only save = %d, want 405", got)
	}
}

// Each setting gets the control its type calls for, and says which layer a
// save writes to and when a higher layer overrides it.
func TestSettingsPageOffersAControlPerKind(t *testing.T) {
	s, _ := newTestServer(t)
	body := signIn(t, s).get("/settings").Body.String()
	for _, want := range []string{
		`action="/settings/value/log_level"`, `<select class="form-select" id="setting-log_level-input"`,
		`id="setting-dry_run-input" name="value" value="true"`, `role="switch"`,
		`type="number" inputmode="numeric" step="1" id="setting-page_size-input" name="value" value="25" min="5" max="500"`,
		`id="setting-git-commit_body-input"`,
		"writes it to the global config",
		// concurrency comes from the environment in the fake engine
		"<code>ICHAVA_CONCURRENCY</code> sets this for the running server",
		// git.base comes from the global config, so it can be reset
		`name="action" value="reset"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("settings page lacks %q", want)
		}
	}
}

package web

import (
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/ichava/icon-sets-sync/src/internal/core/settings"
)

func editorRev(t *testing.T, body string) string {
	t.Helper()
	m := regexp.MustCompile(`name="base" value="([0-9a-f]*)"`).FindStringSubmatch(body)
	if m == nil {
		t.Fatal("no base revision on the page")
	}
	return m[1]
}

// Every save keeps a copy; the history page lists it, shows its diff, and
// restores it -- itself keeping the text it replaces.
func TestConfigHistoryKeepsDiffsAndRestores(t *testing.T) {
	s, eng := newTestServer(t)
	c := signIn(t, s)
	global := filepath.Join(eng.cfg.GlobalHome, settings.AppDir, "config.json")

	save := func(text string) {
		t.Helper()
		rev := editorRev(t, c.get("/config/global").Body.String())
		resp := c.post("/config/global", url.Values{"content": {text}, "base": {rev}})
		if resp.Code != 303 {
			t.Fatalf("save %q = %d:\n%s", text, resp.Code, resp.Body.String())
		}
	}
	save(`{"concurrency": 2}` + "\n")
	save(`{"concurrency": 3}` + "\n")

	list := c.get("/config/global/history").Body.String()
	ids := regexp.MustCompile(`/config/global/history/(\d{8}T[0-9.]+Z)`).FindAllStringSubmatch(list, -1)
	if len(ids) != 1 {
		t.Fatalf("history lists %d copies, want 1 (the first save created the file):\n%s", len(ids), list)
	}
	diff := c.get("/config/global/history/" + ids[0][1]).Body.String()
	if !strings.Contains(diff, `-{&#34;concurrency&#34;: 3}`) || !strings.Contains(diff, `&#43;{&#34;concurrency&#34;: 2}`) {
		t.Errorf("diff page:\n%s", diff)
	}
	resp := c.post("/config/global/restore", url.Values{"id": {ids[0][1]}, "base": {editorRev(t, diff)}})
	if resp.Code != 303 {
		t.Fatalf("restore = %d:\n%s", resp.Code, resp.Body.String())
	}
	if got, _ := os.ReadFile(global); string(got) != `{"concurrency": 2}`+"\n" {
		t.Errorf("after restore: %s", got)
	}
	after := c.get("/config/global/history").Body.String()
	if n := len(regexp.MustCompile(`/config/global/history/\d{8}T`).FindAllString(after, -1)); n != 2 {
		t.Errorf("the restore kept no copy of what it replaced: %d copies", n)
	}
	// A stale base is refused.
	if resp := c.post("/config/global/restore", url.Values{"id": {ids[0][1]}, "base": {"stale"}}); resp.Code != 409 {
		t.Errorf("stale restore = %d", resp.Code)
	}
}

// Fix shows the repaired text and its diff, and writes nothing.
func TestConfigFixProposesAndDoesNotWrite(t *testing.T) {
	s, eng := newTestServer(t)
	c := signIn(t, s)
	global := filepath.Join(eng.cfg.GlobalHome, settings.AppDir, "config.json")
	resp := c.post("/config/global/fix", url.Values{"content": {`{"packages_dir":"/p"}`}, "base": {""}})
	body := resp.Body.String()
	if resp.Code != 200 || !strings.Contains(body, "renamed the deprecated") || !strings.Contains(body, "packages_dirs") {
		t.Fatalf("fix = %d:\n%s", resp.Code, body)
	}
	if _, err := os.Stat(global); err == nil {
		t.Error("fix wrote the file")
	}
}

// A problem names its line, the gutter marks it, and a new global config
// opens on its $schema with the known settings offered.
func TestEditorPointsAtTheLineAndSeedsTheGlobalConfig(t *testing.T) {
	s, _ := newTestServer(t)
	c := signIn(t, s)
	resp := c.post("/config/global/validate", url.Values{"content": {"{\n  \"concurrency\": 2,\n  \"dry_rnu\": true\n}\n"}})
	body := resp.Body.String()
	if !strings.Contains(body, `data-editor-line="3"`) || !strings.Contains(body, "line 3, column 3") {
		t.Errorf("finding has no line:\n%s", body)
	}
	if strings.Count(body, `<li class="badge-bad fw-bold">`) != 1 {
		t.Error("the gutter does not mark exactly the one bad line")
	}
	fresh := c.get("/config/global").Body.String()
	if !strings.Contains(fresh, settings.GlobalSchemaURL) || !strings.Contains(fresh, `data-insert="&#34;packages_dirs&#34;: []"`) {
		t.Errorf("new global editor lacks the seed or the key picker")
	}
	schema := c.get("/schema/global.schema.json")
	if schema.Code != 200 || !strings.Contains(schema.Body.String(), `"packages_dirs"`) {
		t.Errorf("schema = %d", schema.Code)
	}
}

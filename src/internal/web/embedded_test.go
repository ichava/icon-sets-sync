package web

import (
	"net/url"
	"strings"
	"testing"

	"github.com/ichava/icon-sets-sync/src/internal/core/config"
)

// Configs built into the binary cannot be edited: the page says so, offers
// no Save, refuses a save that is posted anyway, and points at --config-dir.
func TestEmbeddedConfigsAreReadOnly(t *testing.T) {
	s, eng := newTestServer(t)
	eng.cfg.ConfigSource = config.SourceEmbedded
	c := signIn(t, s)

	body := c.get("/config/icon-sets-demo.json").Body.String()
	if !strings.Contains(body, "built into this binary") || !strings.Contains(body, "--config-dir") {
		t.Fatalf("the editor does not say the configs are embedded:\n%s", body)
	}
	if strings.Contains(body, `id="editor-save"`) {
		t.Fatal("the editor offers Save on an embedded config")
	}
	doc, err := eng.cfg.ReadConfig("icon-sets-demo.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := eng.cfg.WriteConfig("icon-sets-demo.json", []byte(`{}`), doc.Rev); err == nil {
		t.Fatal("a save into the embedded configs was accepted")
	}
	_ = c.post("/config/icon-sets-demo.json", url.Values{"content": {"{}"}, "base": {doc.Rev}})
	if after, _ := eng.cfg.ReadConfig("icon-sets-demo.json"); string(after.Data) != string(doc.Data) {
		t.Fatal("a posted save changed an embedded config")
	}
}

func TestSettingsNamesTheConfigSource(t *testing.T) {
	e := &AppEngine{ConfigDir: "/x/config", ConfigSource: config.SourceExecutable}
	if got := e.Settings().ConfigSource; got != config.SourceExecutable {
		t.Fatalf("Settings().ConfigSource = %q", got)
	}
}

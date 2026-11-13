package web

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The UI opens pull requests through gh, which keeps its own credential, so a
// GitHub token in the environment must never reach the run history on disk
// or a page this server renders. The token values are generated per run.
func TestNoGitHubTokenReachesDiskOrAPage(t *testing.T) {
	secrets := map[string]string{"GH_TOKEN": NewToken(), "GITHUB_TOKEN": NewToken()}
	for k, v := range secrets {
		t.Setenv(k, v)
	}
	dir := t.TempDir()
	s, _ := newTestServer(t, func(o *Options) { o.HistoryDir = dir })
	c := signIn(t, s)
	path := startRun(t, c, "/packs/icon-sets-flag/sync", nil)
	waitJob(t, s, path)

	pages := []string{"/", "/settings", "/jobs", path, path + "/log.jsonl", "/packs/icon-sets-flag"}
	for _, p := range pages {
		body := c.get(p).Body.String()
		for name, v := range secrets {
			if strings.Contains(body, v) {
				t.Errorf("%s renders the value of %s", p, name)
			}
		}
	}

	files := 0
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		files++
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		for name, v := range secrets {
			if strings.Contains(string(b), v) {
				t.Errorf("%s holds the value of %s", p, name)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// A history directory the run never wrote to would pass vacuously.
	if files == 0 {
		t.Fatal("the run wrote no history, so nothing was inspected")
	}
}

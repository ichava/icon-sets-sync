package web

import (
	"flag"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"
)

var update = flag.Bool("update", false, "rewrite testdata/golden from the current output")

var (
	volatileCSRF  = regexp.MustCompile(`(name="csrf-token" content="|name="csrf" value=")[^"]*"`)
	volatileAsset = regexp.MustCompile(`/assets/[0-9a-f]{12}/`)
	volatileJobID = regexp.MustCompile(`\d{8}T\d{6}Z-[0-9a-f]{6}`)
	volatileTemp  = regexp.MustCompile(`/[^"<>\s]*?/(config|xdg)/`)
)

// normalise replaces what changes between runs, so a golden diff is a change
// in the page and nothing else.
func normalise(body string) string {
	body = volatileCSRF.ReplaceAllString(body, `${1}CSRF"`)
	body = volatileJobID.ReplaceAllString(body, "JOBID")
	body = volatileTemp.ReplaceAllString(body, "/TMP/$1/")
	return volatileAsset.ReplaceAllString(body, "/assets/HASH/")
}

// goldenPages renders one example of every page template.
func goldenPages(t *testing.T) map[string]string {
	t.Helper()
	s, _ := newTestServer(t)
	c := signIn(t, s)
	c.get("/")
	if !s.waitIdle(5 * time.Second) {
		t.Fatal("check did not finish")
	}
	run := c.post("/packs/icon-sets-flag/sync", nil)
	if run.Code != http.StatusSeeOther {
		t.Fatalf("starting a run: %d", run.Code)
	}
	jobPath := run.Header().Get("Location")
	waitJob(t, s, jobPath)

	pages := map[string]string{
		"confirm": normalise(c.post("/packs/icon-sets-flag/sync", url.Values{"mode": {"real"}}).Body.String()),
	}
	for name, path := range map[string]string{
		"dashboard": "/",
		"settings":  "/settings",
		"licenses":  "/about/licenses",
		"error":     "/no-such-page",
		"pack":      "/packs/icon-sets-tabler",
		"job":       jobPath,
		"jobs":      "/jobs",
		"config":    "/config",
		"editor":    "/config/icon-sets-demo.json",
	} {
		rec := c.get(path)
		if name != "error" && rec.Code != http.StatusOK {
			t.Fatalf("%s: %d", path, rec.Code)
		}
		pages[name] = normalise(rec.Body.String())
	}
	return pages
}

func TestGoldenPages(t *testing.T) {
	pages := goldenPages(t)
	dir := filepath.Join("testdata", "golden")
	if *update {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		for name, body := range pages {
			if err := os.WriteFile(filepath.Join(dir, name+".html"), []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	for name, body := range pages {
		want, err := os.ReadFile(filepath.Join(dir, name+".html"))
		if err != nil {
			t.Fatalf("%s: %v (run go test ./internal/web -update)", name, err)
		}
		if string(want) != body {
			t.Errorf("%s differs from its golden file; review the change and run go test ./internal/web -update", name)
		}
	}
}

// Every page template has a golden file, so an untested page fails here.
func TestEveryPageTemplateHasAGoldenFile(t *testing.T) {
	names, err := pageNames()
	if err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(filepath.Join("testdata", "golden"))
	if err != nil {
		t.Fatal(err)
	}
	var golden []string
	for _, e := range entries {
		golden = append(golden, strings.TrimSuffix(e.Name(), ".html"))
	}
	sort.Strings(golden)
	if strings.Join(names, ",") != strings.Join(golden, ",") {
		t.Fatalf("page templates %v, golden files %v", names, golden)
	}
	pages := goldenPages(t)
	if len(pages) != len(names) {
		t.Fatalf("rendered %d pages for %d templates", len(pages), len(names))
	}
}

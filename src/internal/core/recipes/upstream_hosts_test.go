package recipes

import (
	"net/url"
	"path/filepath"
	"sort"
	"testing"

	"github.com/ichava/icon-sets-sync/src/internal/core/config"
	"github.com/ichava/icon-sets-sync/src/internal/core/httpx"
	"github.com/ichava/icon-sets-sync/src/internal/core/pipeline"
	"github.com/ichava/icon-sets-sync/src/internal/core/sources"
)

// upstreamURLs lists every URL a source reaches through the HTTP client.
func upstreamURLs(src pipeline.Source) []string {
	switch s := src.(type) {
	case sources.Compound:
		return append(upstreamURLs(s.First), upstreamURLs(s.Second)...)
	case sources.GithubArchive:
		return []string{s.URL()}
	case sources.UnicodeCldr:
		return []string{s.URL()}
	}
	return nil
}

// P2-G5's guard against breaking real syncs. The allowlist is a default-deny
// list, so a recipe that starts fetching from a new host would fail in
// production and pass every other test. This loads every shipped pack config,
// builds the pipelines its recipe builds, and asks the real client whether it
// would let each URL through. It also checks the other direction: a default
// host no shipped recipe reaches is a stale entry, a standing hole for nothing.
func TestEveryShippedUpstreamIsAllowedByDefault(t *testing.T) {
	configDir := filepath.Join("..", "..", "..", "..", "config")
	registry, err := config.LoadRegistry(configDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(registry.Packs) < 4 {
		t.Fatalf("inspected %d shipped packs, want at least 4: the glob is not finding them", len(registry.Packs))
	}

	client := httpx.New()
	reached := map[string]bool{}
	urls := 0

	check := func(pack, what, raw string) {
		t.Helper()
		urls++
		if err := client.CheckURL(raw); err != nil {
			t.Errorf("%s: %s %s is refused by the default allowlist: %v", pack, what, raw, err)
		}
		if u, err := url.Parse(raw); err == nil {
			reached[u.Hostname()] = true
		}
	}

	for _, slug := range registry.Packs {
		pack, err := config.LoadPack(slug, configDir)
		if err != nil {
			t.Fatal(err)
		}
		if pack.VersionCheckURL != "" {
			check(slug, "version_check_url", pack.VersionCheckURL)
		}

		version := pack.CurrentVersion
		if version == "" {
			version = "1.0.0"
		}
		pipelines, err := Build(pack, version, Options{DryRun: true})
		if err != nil {
			t.Fatalf("%s: %v", slug, err)
		}
		for _, p := range pipelines {
			for _, stage := range p.Stages() {
				if src, ok := stage.(pipeline.Source); ok {
					for _, raw := range upstreamURLs(src) {
						check(slug, p.Name(), raw)
					}
				}
			}
		}
		// An npm source is fetched by `npm pack`, outside the client, from the
		// public registry: it is not a URL this test can read, so name it.
		if pack.Source.Type == "npm" {
			check(slug, "npm registry", "https://registry.npmjs.org/"+pack.Source.Package)
		}
	}

	if urls < 8 {
		t.Fatalf("inspected %d upstream URLs, want at least 8: the walk is not finding sources", urls)
	}

	var stale []string
	for _, host := range httpx.DefaultAllowedHosts() {
		if !reached[host] {
			stale = append(stale, host)
		}
	}
	sort.Strings(stale)
	if len(stale) > 0 {
		// github.com's archive redirects to codeload.github.com, which no
		// config names. It is covered by the redirect test in httpx.
		if len(stale) != 1 || stale[0] != "codeload.github.com" {
			t.Errorf("default allowlist hosts no shipped recipe reaches: %v", stale)
		}
	}
}

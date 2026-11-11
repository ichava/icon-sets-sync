package checker

import (
	"path/filepath"
	"regexp"
	"testing"

	"github.com/ichava/icon-sets-sync/src/internal/core/config"
)

// R1: a github-archive pack names a GitHub endpoint as its version_check_url
// (a tag list, a release, or a branch head), and ParseLatest used to answer ""
// for every one of them. So `check` reported "could not parse latest" for nine
// shipped packs and `sync` refused them all with "cannot resolve the latest
// upstream version". The version is read from the payload's shape, and the
// tag prefix comes from archive_url, so the result is exactly the value
// archive_url's {version} expects.
func TestParseLatestGitHubArchive(t *testing.T) {
	archive := func(u string) config.SourceConfig {
		return config.SourceConfig{Type: "github-archive", ArchiveURL: u}
	}
	for _, tc := range []struct {
		name   string
		source config.SourceConfig
		body   string
		want   string
	}{
		{"tag list, v prefix", archive("https://github.com/o/r/archive/refs/tags/v{version}.tar.gz"),
			`[{"name":"v4.0.0"},{"name":"v3.1.0"}]`, "4.0.0"},
		{"tag list, scoped prefix", archive("https://github.com/o/r/archive/refs/tags/website@{version}.tar.gz"),
			`[{"name":"website@0.0.17"},{"name":"website@0.0.16"}]`, "0.0.17"},
		{"tag list skips tags without the prefix", archive("https://github.com/o/r/archive/refs/tags/website@{version}.tar.gz"),
			`[{"name":"other@9.9.9"},{"name":"website@0.0.17"}]`, "0.0.17"},
		{"tag list, bare tags", archive("https://github.com/o/r/archive/refs/tags/{version}.zip"),
			`[{"name":"7.3.1"}]`, "7.3.1"},
		{"release, v prefix", archive("https://github.com/o/r/archive/refs/tags/v{version}.tar.gz"),
			`{"tag_name":"v2026-09-24-unicode18_0","html_url":"https://x.test/r"}`, "2026-09-24-unicode18_0"},
		{"release, bare tag", archive("https://github.com/o/r/archive/refs/tags/{version}.zip"),
			`{"tag_name":"7.3.1"}`, "7.3.1"},
		{"branch head is a commit", archive("https://github.com/o/r/archive/{version}.tar.gz"),
			`{"sha":"82d948812bfe3f269ef8f731dcdb07b08160edc4","commit":{}}`, "82d948812bfe3f269ef8f731dcdb07b08160edc4"},
		{"empty tag list", archive("https://github.com/o/r/archive/refs/tags/v{version}.tar.gz"), `[]`, ""},
		{"unknown shape", archive("https://github.com/o/r/archive/{version}.zip"), `{"version":"1"}`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ParseLatest(tc.source, payload(t, tc.body)); got != tc.want {
				t.Errorf("ParseLatest = %q, want %q", got, tc.want)
			}
		})
	}
}

// Every shipped github-archive config names an endpoint ParseLatest reads.
// Counted, so a glob that stops matching fails rather than passing empty.
func TestShippedArchiveConfigsNamePollableEndpoints(t *testing.T) {
	pollable := regexp.MustCompile(`^https://api\.github\.com/repos/[^/]+/[^/]+/(tags|releases/latest|releases/tags/[^/]+|commits/[^/]+)$`)
	files, err := filepath.Glob(filepath.Join("..", "..", "..", "..", "config", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, f := range files {
		if filepath.Base(f) == "packs.json" {
			continue
		}
		pack, err := config.LoadPackFile(f)
		if err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		if pack.Source.Type != "github-archive" {
			continue
		}
		n++
		if !pollable.MatchString(pack.VersionCheckURL) {
			t.Errorf("%s: version_check_url %q is not a tag list, release or branch head", filepath.Base(f), pack.VersionCheckURL)
		}
	}
	if n < 9 {
		t.Fatalf("inspected %d github-archive configs, want at least 9", n)
	}
}

func TestCheckPackCarriesTheConfigName(t *testing.T) {
	c := &Checker{Fetch: stubFetcher{body: `{"version":"1.0.0"}`}}
	p := packWithVendored(t, "1.0.0")
	p.Name = "icon-sets-extras-ant-design"
	if got := c.CheckPack(t.Context(), p); got.Name != p.Name {
		t.Errorf("Name = %q, want %q", got.Name, p.Name)
	}
}

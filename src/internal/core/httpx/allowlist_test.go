package httpx

import (
	"context"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// fetchers runs the same URL through both entry points, because the rule has
// to hold for every fetch and a guard on only one of them is the bug.
var fetchers = map[string]func(c *Client, url, dest string) error{
	"GetJSON": func(c *Client, url, _ string) error {
		var out map[string]any
		return c.GetJSON(context.Background(), url, &out)
	},
	"Download": func(c *Client, url, dest string) error {
		_, err := c.Download(context.Background(), url, dest, false)
		return err
	},
}

// P2-G6: an upstream URL is https. Plain http is allowed only to loopback, so a
// test can stand up an httptest server.
func TestUpstreamURLsMustBeHTTPS(t *testing.T) {
	cases := []struct {
		name string
		url  string
		ok   bool
	}{
		{"https", "https://registry.npmjs.org/x", true},
		{"http to a public host", "http://registry.npmjs.org/x", false},
		{"http to an allowed host's CDN", "http://codeload.github.com/x", false},
		{"ftp", "ftp://registry.npmjs.org/x", false},
		{"file", "file:///etc/passwd", false},
		{"no scheme", "registry.npmjs.org/x", false},
		{"http to 127.0.0.1", "http://127.0.0.1:8765/latest.json", true},
		{"http to localhost", "http://localhost:8765/latest.json", true},
		{"http to ::1", "http://[::1]:8765/latest.json", true},
		{"https to 127.0.0.1", "https://127.0.0.1:8765/latest.json", true},
		{"a loopback look-alike host", "http://localhost.evil.example/x", false},
		{"another loopback address is not exempt", "http://127.0.0.2/x", false},
	}
	for name, fetch := range fetchers {
		for _, tc := range cases {
			t.Run(name+"/"+tc.name, func(t *testing.T) {
				c, tr, _ := newTestClient(stubResponse{status: 200, body: `{"ok":true}`})
				err := fetch(c, tc.url, filepath.Join(t.TempDir(), "f"))
				if tc.ok {
					if err != nil {
						t.Fatalf("%s refused: %v", tc.url, err)
					}
					return
				}
				if err == nil {
					t.Fatalf("%s was fetched", tc.url)
				}
				if tr.hits != 0 {
					t.Errorf("a refused URL still sent %d request(s)", tr.hits)
				}
				if tc.name != "a loopback look-alike host" && tc.name != "another loopback address is not exempt" &&
					!strings.Contains(err.Error(), tc.url) {
					t.Errorf("error does not name the URL %q: %v", tc.url, err)
				}
			})
		}
	}
}

func TestPlainHTTPErrorNamesTheRule(t *testing.T) {
	c, _, _ := newTestClient(stubResponse{status: 200, body: `{}`})
	err := fetchers["GetJSON"](c, "http://registry.npmjs.org/x", "")
	if err == nil {
		t.Fatal("http was fetched")
	}
	for _, want := range []string{"http://registry.npmjs.org/x", "https", "127.0.0.1"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q lacks %q", err, want)
		}
	}
}

// P2-G5: the host must be on the allowlist, for every fetch.
func TestHostAllowlist(t *testing.T) {
	cases := []struct {
		name  string
		url   string
		extra []string
		ok    bool
	}{
		{"github.com", "https://github.com/o/r/archive/refs/tags/1.zip", nil, true},
		{"api.github.com", "https://api.github.com/repos/o/r/releases/latest", nil, true},
		{"codeload.github.com", "https://codeload.github.com/o/r/zip/refs/tags/1", nil, true},
		{"registry.npmjs.org", "https://registry.npmjs.org/flag-icons/latest", nil, true},
		{"unicode.org", "https://unicode.org/Public/emoji/16.0/emoji-test.txt", nil, true},
		{"case is ignored", "https://GitHub.COM/o/r", nil, true},
		{"a port is ignored", "https://github.com:443/o/r", nil, true},
		{"a trailing dot is ignored", "https://github.com./o/r", nil, true},
		{"an unknown host", "https://evil.example/x", nil, false},
		{"a suffix look-alike", "https://evilgithub.com/x", nil, false},
		{"a prefix look-alike", "https://github.com.evil.example/x", nil, false},
		{"a subdomain is not the parent", "https://gist.github.com/x", nil, false},
		{"userinfo does not smuggle a host", "https://github.com@evil.example/x", nil, false},
		{"a CDN no shipped recipe uses", "https://objects.githubusercontent.com/x", nil, false},
		{"an ip literal", "https://93.184.216.34/x", nil, false},
		{"extended by the caller", "https://mirror.example/x", []string{"mirror.example"}, true},
		{"extension is case-insensitive", "https://mirror.example/x", []string{"Mirror.Example"}, true},
		{"extension does not widen to subdomains", "https://a.mirror.example/x", []string{"mirror.example"}, false},
	}
	for name, fetch := range fetchers {
		for _, tc := range cases {
			t.Run(name+"/"+tc.name, func(t *testing.T) {
				c, tr, _ := newTestClient(stubResponse{status: 200, body: `{"ok":true}`})
				c.AllowedHosts = tc.extra
				err := fetch(c, tc.url, filepath.Join(t.TempDir(), "f"))
				if tc.ok {
					if err != nil {
						t.Fatalf("%s refused: %v", tc.url, err)
					}
					return
				}
				if err == nil {
					t.Fatalf("%s was fetched", tc.url)
				}
				if tr.hits != 0 {
					t.Errorf("a refused host still sent %d request(s)", tr.hits)
				}
			})
		}
	}
}

func TestBlockedHostErrorNamesTheHostAndHowToAllowIt(t *testing.T) {
	c, _, _ := newTestClient(stubResponse{status: 200, body: `{}`})
	err := fetchers["GetJSON"](c, "https://mirror.example/x", "")
	if err == nil {
		t.Fatal("an unlisted host was fetched")
	}
	for _, want := range []string{"mirror.example", "allowed_hosts", "ICHAVA_ALLOWED_HOSTS"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q lacks %q", err, want)
		}
	}
}

// A blocked fetch is a configuration problem. Retrying it four times with a
// backoff would only delay the message.
func TestABlockedFetchIsNotRetried(t *testing.T) {
	c, tr, waits := newTestClient(stubResponse{status: 503})
	_ = fetchers["GetJSON"](c, "https://evil.example/x", "")
	if tr.hits != 0 || len(*waits) != 0 {
		t.Fatalf("hits=%d waits=%d", tr.hits, len(*waits))
	}
}

// Every hop of a redirect chain is a fetch, so each must pass the same rules.
func TestRedirectsAreHeldToTheAllowlist(t *testing.T) {
	cases := []struct {
		name     string
		from     string
		location string
		extra    []string
		ok       bool
	}{
		{"github to codeload", "https://github.com/o/r/archive/1.zip", "https://codeload.github.com/o/r/zip/1", nil, true},
		{"to an unlisted host", "https://github.com/o/r/archive/1.zip", "https://evil.example/x", nil, false},
		{"to a listed extra host", "https://github.com/o/r/archive/1.zip", "https://cdn.mirror.example/x", []string{"cdn.mirror.example"}, true},
		{"loopback start to an unlisted host", "http://127.0.0.1/a", "https://evil.example/a", nil, false},
		{"loopback is not a redirect target by name", "https://github.com/a", "https://localhost/a", nil, false},
		{"loopback http is not a redirect target", "https://github.com/a", "http://localhost/a", nil, false},
	}
	for name, fetch := range fetchers {
		for _, tc := range cases {
			t.Run(name+"/"+tc.name, func(t *testing.T) {
				c, _, _ := newTestClient(
					stubResponse{status: 302, location: tc.location},
					stubResponse{status: 200, body: `{"ok":true}`},
				)
				c.AllowedHosts = tc.extra
				err := fetch(c, tc.from, filepath.Join(t.TempDir(), "f"))
				if tc.ok && err != nil {
					t.Fatalf("redirect to %s refused: %v", tc.location, err)
				}
				if !tc.ok {
					if err == nil {
						t.Fatalf("redirect to %s followed", tc.location)
					}
					if !strings.Contains(err.Error(), "redirect") {
						t.Errorf("error does not say it was a redirect: %v", err)
					}
				}
			})
		}
	}
}

func TestDefaultAllowedHostsAreExactlyTheShippedSet(t *testing.T) {
	want := []string{"api.github.com", "codeload.github.com", "github.com", "registry.npmjs.org", "unicode.org"}
	if got := DefaultAllowedHosts(); !reflect.DeepEqual(got, want) {
		t.Fatalf("DefaultAllowedHosts = %v, want %v", got, want)
	}
	// The returned slice is a copy: editing it must not widen the allowlist.
	DefaultAllowedHosts()[0] = "evil.example"
	c, _, _ := newTestClient(stubResponse{status: 200, body: `{}`})
	if err := fetchers["GetJSON"](c, "https://evil.example/x", ""); err == nil {
		t.Fatal("mutating the returned slice widened the allowlist")
	}
}

func TestSetExtraAllowedHostsAppliesToNewClients(t *testing.T) {
	t.Cleanup(func() { SetExtraAllowedHosts(nil) })
	c, _, _ := newTestClient(stubResponse{status: 200, body: `{}`})
	if err := fetchers["GetJSON"](c, "https://mirror.example/x", ""); err == nil {
		t.Fatal("fetched before the host was allowed")
	}

	SetExtraAllowedHosts([]string{"mirror.example"})
	c, _, _ = newTestClient(stubResponse{status: 200, body: `{}`})
	if err := fetchers["GetJSON"](c, "https://mirror.example/x", ""); err != nil {
		t.Fatalf("refused after the host was allowed: %v", err)
	}

	SetExtraAllowedHosts(nil)
	c, _, _ = newTestClient(stubResponse{status: 200, body: `{}`})
	if err := fetchers["GetJSON"](c, "https://mirror.example/x", ""); err == nil {
		t.Fatal("still allowed after the extras were cleared")
	}
}

func TestParseHostList(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"  ", nil},
		{"a.example", []string{"a.example"}},
		{"A.Example, b.example", []string{"a.example", "b.example"}},
		{"a.example b.example", []string{"a.example", "b.example"}},
		{"a.example,,b.example,", []string{"a.example", "b.example"}},
		{"a.example, a.example", []string{"a.example"}},
	}
	for _, tc := range cases {
		if got := ParseHostList(tc.in); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("ParseHostList(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestCheckSchemeIsTheConfigTimeRule(t *testing.T) {
	cases := []struct {
		url string
		ok  bool
	}{
		{"https://registry.npmjs.org/x", true},
		{"https://github.com/o/r/archive/refs/tags/{version}.zip", true},
		{"http://127.0.0.1:8765/latest.json", true},
		{"http://localhost/x", true},
		{"http://[::1]:1/x", true},
		{"http://registry.npmjs.org/x", false},
		{"ftp://registry.npmjs.org/x", false},
		{"//registry.npmjs.org/x", false},
	}
	for _, tc := range cases {
		err := CheckScheme(tc.url)
		if tc.ok && err != nil {
			t.Errorf("CheckScheme(%q) = %v", tc.url, err)
		}
		if !tc.ok {
			if err == nil {
				t.Errorf("CheckScheme(%q) allowed it", tc.url)
			} else if !strings.Contains(err.Error(), tc.url) {
				t.Errorf("CheckScheme(%q) error does not name the URL: %v", tc.url, err)
			}
		}
	}
}

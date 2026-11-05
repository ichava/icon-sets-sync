package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// TestTokenSwapNeverRedirectsOffSite is go/unvalidated-url-redirection: the
// launch-token exchange redirects to the request's own path, and a path that
// starts with two slashes (or a slash and a backslash, which browsers read the
// same way) is a scheme-relative URL naming another host. The redirect must
// stay a same-origin, relative path whatever the request asked for.
func TestTokenSwapNeverRedirectsOffSite(t *testing.T) {
	s, _ := newTestServer(t)
	for _, path := range []string{
		"//evil.example/x",
		"///evil.example/x",
		"/%2F/evil.example/x",
		`/\evil.example/x`,
	} {
		r := httptest.NewRequest(http.MethodGet, "http://"+testHost+"/", nil)
		r.URL.Path = path
		r.URL.RawPath = ""
		q := url.Values{"token": {testToken}}
		r.URL.RawQuery = q.Encode()
		rec := do(s, r)
		loc := rec.Header().Get("Location")
		if !strings.HasPrefix(loc, "/") || strings.HasPrefix(loc, "//") || strings.HasPrefix(loc, `/\`) {
			t.Errorf("path %q redirected to %q, which is not a same-origin relative path", path, loc)
			continue
		}
		if u, err := url.Parse(loc); err != nil || u.Host != "" || u.Scheme != "" {
			t.Errorf("path %q redirected to %q (host %q)", path, loc, u.Host)
		}
	}

	// The ordinary case is unchanged: the same path, the token stripped.
	rec := do(s, httptest.NewRequest(http.MethodGet, "http://"+testHost+"/settings?token="+testToken+"&theme=dark", nil))
	if loc := rec.Header().Get("Location"); loc != "/settings?theme=dark" {
		t.Errorf("ordinary redirect = %q", loc)
	}
}

func TestIsLocalURL(t *testing.T) {
	for target, want := range map[string]bool{
		"/":                                true,
		"/settings?theme=dark":             true,
		"/config/icon-sets-lucide?saved=1": true,
		"":                                 false,
		"settings":                         false,
		"//evil.example/x":                 false,
		`/\evil.example/x`:                 false,
		"https://evil.example":             false,
		"/x\r\nLocation: //e":              false,
		"/x\x00":                           false,
	} {
		if got := isLocalURL(target); got != want {
			t.Errorf("isLocalURL(%q) = %v, want %v", target, got, want)
		}
	}
}

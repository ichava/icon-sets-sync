package httpx

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// R5: ten shipped packs poll api.github.com, and GetJSON never sent a token,
// so every check shared GitHub's 60-requests-an-hour anonymous limit and a
// second check in the hour failed with a bare "403 Forbidden" -- even in CI,
// where sync-pack.yml already exports GH_TOKEN.
func TestGetJSONSendsAGitHubTokenToTheGitHubAPIOnly(t *testing.T) {
	tok := "test" + strings.Repeat("x", 8)
	t.Setenv("GH_TOKEN", tok)
	t.Setenv("GITHUB_TOKEN", "")
	seen := map[string]string{}
	c := New()
	c.Sleep = func(time.Duration) {}
	c.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		seen[req.URL.Host] = req.Header.Get("Authorization")
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{}`)),
			Header: make(http.Header), Request: req}, nil
	})
	var out map[string]any
	for _, u := range []string{"https://api.github.com/repos/o/r/tags", "https://registry.npmjs.org/x"} {
		if err := c.GetJSON(context.Background(), u, &out); err != nil {
			t.Fatal(err)
		}
	}
	if seen["api.github.com"] != "Bearer "+tok {
		t.Errorf("api.github.com Authorization = %q", seen["api.github.com"])
	}
	if seen["registry.npmjs.org"] != "" {
		t.Errorf("the token went to registry.npmjs.org: %q", seen["registry.npmjs.org"])
	}
}

func TestGitHubTokenFallsBackToGITHUB_TOKEN(t *testing.T) {
	t.Setenv("GH_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "second")
	if got := githubToken(); got != "second" {
		t.Errorf("githubToken = %q", got)
	}
	t.Setenv("GITHUB_TOKEN", "")
	if got := githubToken(); got != "" {
		t.Errorf("githubToken = %q with neither set", got)
	}
}

// An exhausted rate limit says so, and says how to lift it, instead of a
// bare "403 Forbidden".
func TestGetJSONExplainsAnExhaustedGitHubRateLimit(t *testing.T) {
	t.Setenv("GH_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "")
	c := New()
	c.Sleep = func(time.Duration) {}
	c.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		h := make(http.Header)
		h.Set("X-RateLimit-Remaining", "0")
		h.Set("X-RateLimit-Reset", "1794200000")
		return &http.Response{StatusCode: 403, Status: "403 Forbidden",
			Body: io.NopCloser(strings.NewReader(`{"message":"API rate limit exceeded"}`)), Header: h, Request: req}, nil
	})
	var out map[string]any
	err := c.GetJSON(context.Background(), "https://api.github.com/repos/o/r/tags", &out)
	if err == nil {
		t.Fatal("no error")
	}
	for _, want := range []string{"rate limit", "GH_TOKEN", "resets"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q lacks %q", err, want)
		}
	}
}

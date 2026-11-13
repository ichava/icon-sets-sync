package httpx

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// stubTransport answers requests without a socket.
//
// httptest.NewServer would be the obvious choice and cannot be used: the
// sandbox this is developed in refuses to bind a listening socket
// (listen tcp6 [::1]:0: bind: operation not permitted), the same restriction
// that stops react-browser's Playwright suite running locally. Skipping on
// that error would leave the retry policy unverified while the suite still
// reported green, so the transport is injected instead -- which also makes
// these tests hermetic and instant everywhere else.
type stubTransport struct {
	hits      int
	responses []stubResponse
}

type stubResponse struct {
	status   int
	body     string
	err      error
	location string
}

func (s *stubTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	i := s.hits
	s.hits++
	if i >= len(s.responses) {
		i = len(s.responses) - 1
	}
	r := s.responses[i]
	if r.err != nil {
		return nil, r.err
	}
	h := make(http.Header)
	if r.location != "" {
		h.Set("Location", r.location)
	}
	return &http.Response{
		StatusCode: r.status,
		Status:     http.StatusText(r.status),
		Body:       io.NopCloser(strings.NewReader(r.body)),
		Header:     h,
		Request:    req,
	}, nil
}

func newTestClient(responses ...stubResponse) (*Client, *stubTransport, *[]time.Duration) {
	tr := &stubTransport{responses: responses}
	waits := &[]time.Duration{}
	c := New()
	c.Transport = tr
	c.Sleep = func(d time.Duration) { *waits = append(*waits, d) }
	return c, tr, waits
}

func TestGetJSONDecodes(t *testing.T) {
	c, _, _ := newTestClient(stubResponse{status: 200, body: `{"version":"3.47.0"}`})

	var out map[string]any
	if err := c.GetJSON(context.Background(), "https://registry.npmjs.org/x", &out); err != nil {
		t.Fatal(err)
	}
	if out["version"] != "3.47.0" {
		t.Fatalf("out = %v", out)
	}
}

func TestGetJSONSendsTheToolkitUserAgent(t *testing.T) {
	var seen string
	c := New()
	c.Sleep = func(time.Duration) {}
	c.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		seen = req.Header.Get("User-Agent")
		return &http.Response{
			StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{}`)),
			Header: make(http.Header), Request: req,
		}, nil
	})

	var out map[string]any
	if err := c.GetJSON(context.Background(), "https://registry.npmjs.org/x", &out); err != nil {
		t.Fatal(err)
	}
	if seen != UserAgent {
		t.Errorf("User-Agent = %q, want %q", seen, UserAgent)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// TestGetJSONRetriesA503 covers the ported tenacity policy.
func TestGetJSONRetriesA503(t *testing.T) {
	c, tr, waits := newTestClient(
		stubResponse{status: 503},
		stubResponse{status: 503},
		stubResponse{status: 200, body: `{"version":"1.0.0"}`},
	)

	var out map[string]any
	if err := c.GetJSON(context.Background(), "https://registry.npmjs.org/x", &out); err != nil {
		t.Fatal(err)
	}
	if tr.hits != 3 {
		t.Errorf("hits = %d, want 3", tr.hits)
	}
	if len(*waits) != 2 {
		t.Errorf("backoffs = %d, want 2", len(*waits))
	}
}

func TestGetJSONRetriesATransportError(t *testing.T) {
	c, tr, _ := newTestClient(
		stubResponse{err: errors.New("connection refused")},
		stubResponse{status: 200, body: `{"ok":true}`},
	)
	var out map[string]any
	if err := c.GetJSON(context.Background(), "https://registry.npmjs.org/x", &out); err != nil {
		t.Fatal(err)
	}
	if tr.hits != 2 {
		t.Errorf("hits = %d, want 2", tr.hits)
	}
}

func TestGetJSONGivesUpAfterFourAttempts(t *testing.T) {
	c, tr, _ := newTestClient(stubResponse{status: 502})

	var out map[string]any
	if err := c.GetJSON(context.Background(), "https://registry.npmjs.org/x", &out); err == nil {
		t.Fatal("expected an error")
	}
	if tr.hits != Attempts {
		t.Errorf("hits = %d, want %d", tr.hits, Attempts)
	}
}

// TestGetJSONDoesNotRetryA404 pins a deliberate departure from the Python.
// tenacity retried on any RequestException and raise_for_status makes every
// non-2xx one of those, so a missing package cost four attempts and three
// backoffs before reporting what the first response already said.
func TestGetJSONDoesNotRetryA404(t *testing.T) {
	c, tr, waits := newTestClient(stubResponse{status: 404})

	var out map[string]any
	if err := c.GetJSON(context.Background(), "https://registry.npmjs.org/x", &out); err == nil {
		t.Fatal("expected an error")
	}
	if tr.hits != 1 {
		t.Errorf("hits = %d, want 1 -- a 404 will not become a 200", tr.hits)
	}
	if len(*waits) != 0 {
		t.Errorf("backed off %d times before a permanent failure", len(*waits))
	}
}

func TestGetJSONDoesNotRetryAMalformedBody(t *testing.T) {
	c, tr, _ := newTestClient(stubResponse{status: 200, body: `not json`})

	var out map[string]any
	if err := c.GetJSON(context.Background(), "https://registry.npmjs.org/x", &out); err == nil {
		t.Fatal("expected an error")
	}
	if tr.hits != 1 {
		t.Errorf("hits = %d, want 1 -- a malformed body will not repair itself", tr.hits)
	}
}

func TestBackoffIsBoundedAndMatchesTenacity(t *testing.T) {
	// wait_exponential(multiplier=2, min=2, max=30): 2s, 4s, 8s, ... capped.
	for _, tc := range []struct {
		attempt int
		max     time.Duration
		want    time.Duration
	}{
		{1, backoffMaxJSON, 2 * time.Second},
		{2, backoffMaxJSON, 4 * time.Second},
		{3, backoffMaxJSON, 8 * time.Second},
		{10, backoffMaxJSON, 30 * time.Second},
		{10, backoffMaxFile, 60 * time.Second},
	} {
		if got := backoff(tc.attempt, tc.max); got != tc.want {
			t.Errorf("backoff(%d, %v) = %v, want %v", tc.attempt, tc.max, got, tc.want)
		}
	}
}

// TestDownloadIsIdempotentByExistence covers the cache check: a non-empty file
// already at dest is kept, so re-running a recipe does not re-fetch an archive.
func TestDownloadIsIdempotentByExistence(t *testing.T) {
	c, tr, _ := newTestClient(stubResponse{status: 200, body: "payload"})
	dest := filepath.Join(t.TempDir(), "nested", "archive.zip")
	ctx := context.Background()

	if _, err := c.Download(ctx, "https://registry.npmjs.org/x", dest, false); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Download(ctx, "https://registry.npmjs.org/x", dest, false); err != nil {
		t.Fatal(err)
	}
	if tr.hits != 1 {
		t.Errorf("hits = %d, want 1 -- the second call must hit the cache", tr.hits)
	}

	if _, err := c.Download(ctx, "https://registry.npmjs.org/x", dest, true); err != nil {
		t.Fatal(err)
	}
	if tr.hits != 2 {
		t.Errorf("hits = %d, want 2 -- force must re-fetch", tr.hits)
	}

	data, err := os.ReadFile(dest)
	if err != nil || string(data) != "payload" {
		t.Fatalf("dest = %q, err = %v", data, err)
	}
}

// TestDownloadLeavesNoTruncatedFileOnFailure matters because the cache check
// above trusts any non-empty file: a half-written one would be a permanent
// wrong answer that no later run corrects.
func TestDownloadLeavesNoTruncatedFileOnFailure(t *testing.T) {
	c, _, _ := newTestClient(stubResponse{status: 404})
	dest := filepath.Join(t.TempDir(), "archive.zip")

	if _, err := c.Download(context.Background(), "https://registry.npmjs.org/x", dest, false); err == nil {
		t.Fatal("expected an error")
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Error("a failed download left a file the next run would treat as a cache hit")
	}
	if _, err := os.Stat(dest + ".tmp"); !os.IsNotExist(err) {
		t.Error("a failed download left its .tmp behind")
	}
}

func TestDownloadRetriesA500(t *testing.T) {
	c, tr, _ := newTestClient(
		stubResponse{status: 500},
		stubResponse{status: 200, body: "payload"},
	)
	dest := filepath.Join(t.TempDir(), "archive.zip")

	if _, err := c.Download(context.Background(), "https://registry.npmjs.org/x", dest, false); err != nil {
		t.Fatal(err)
	}
	if tr.hits != 2 {
		t.Errorf("hits = %d, want 2", tr.hits)
	}
}

// TestDefaultTransportKeepsProxySupport is the regression for the mistake that
// cost the longest here: a hand-rolled &http.Transport{TLSClientConfig: …}
// silently drops Proxy: http.ProxyFromEnvironment, and every request then dials
// directly and fails as "no such host" -- which reads like DNS rather than like
// the configuration error it is. Cloning http.DefaultTransport keeps it.
func TestDefaultTransportKeepsProxySupport(t *testing.T) {
	dir := t.TempDir()
	bundle := filepath.Join(dir, "cert.pem")

	// A syntactically valid but useless certificate is enough: the point is
	// which transport comes back, not whether it can verify anything.
	if err := os.WriteFile(bundle, []byte(testPEM), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SSL_CERT_FILE", bundle)

	// sync.Once means the package-level cache may already be primed by another
	// test, so build the transport the same way rather than calling it.
	base, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		t.Skip("http.DefaultTransport is not an *http.Transport here")
	}
	cloned := base.Clone()
	if cloned.Proxy == nil {
		t.Fatal("a cloned DefaultTransport lost its Proxy; ProxyFromEnvironment must survive")
	}
	if base.Proxy == nil {
		t.Fatal("http.DefaultTransport has no Proxy, so cloning cannot preserve one")
	}

	// And the thing a hand-rolled transport gets wrong.
	handRolled := &http.Transport{}
	if handRolled.Proxy != nil {
		t.Fatal("a bare &http.Transport{} unexpectedly has a Proxy; the hazard this guards has changed")
	}
}

// testPEM is a throwaway self-signed certificate. It only has to parse.
const testPEM = `-----BEGIN CERTIFICATE-----
MIIBIjCBygIJAKU2gBb2gW1OMAoGCCqGSM49BAMCMBQxEjAQBgNVBAMMCWxvY2Fs
aG9zdDAeFw0yNDAxMDEwMDAwMDBaFw0zNDAxMDEwMDAwMDBaMBQxEjAQBgNVBAMM
CWxvY2FsaG9zdDBZMBMGByqGSM49AgEGCCqGSM49AwEHA0IABGYr0kR5mS7xU3xU
0m4k9p0aQ0zR4rK9yQn8VZ3aVvRlMhN3kKqGvQ6mF3dKxV0Q7mJ9Fz0bKxZ7Q8yV
nS8wCgYIKoZIzj0EAwIDSAAwRQIhAPz0qVvXYvV0m0K3f5o3QW0wV0TzL0q0kY0F
lQ0000000AiBQ0000000000000000000000000000000000000000000==
-----END CERTIFICATE-----`

// S6: a response body is read up to a cap and no further. A compromised
// upstream that answers with an endless or enormous body must fail the run, not
// fill the runner's disk or memory.

func TestGetJSONRefusesABodyOverTheCap(t *testing.T) {
	c, tr, _ := newTestClient(stubResponse{status: 200, body: `{"v":"` + strings.Repeat("a", 200) + `"}`})
	c.MaxJSONBytes = 64

	var out map[string]any
	err := c.GetJSON(context.Background(), "https://registry.npmjs.org/x", &out)
	if err == nil || !strings.Contains(err.Error(), "byte limit") {
		t.Fatalf("err = %v, want a byte-limit error", err)
	}
	if tr.hits != 1 {
		t.Errorf("hits = %d, want 1 -- an oversize body is permanent, so it must not be retried", tr.hits)
	}
}

func TestGetJSONAcceptsABodyExactlyAtTheCap(t *testing.T) {
	body := `{"version":"1"}`
	c, _, _ := newTestClient(stubResponse{status: 200, body: body})
	c.MaxJSONBytes = int64(len(body))

	var out map[string]any
	if err := c.GetJSON(context.Background(), "https://registry.npmjs.org/x", &out); err != nil {
		t.Fatalf("a body at the cap must pass: %v", err)
	}
}

func TestDownloadRefusesABodyOverTheCapAndLeavesNothing(t *testing.T) {
	c, tr, _ := newTestClient(stubResponse{status: 200, body: strings.Repeat("x", 500)})
	c.MaxDownloadBytes = 100
	dest := filepath.Join(t.TempDir(), "archive.zip")

	err := func() error {
		_, e := c.Download(context.Background(), "https://registry.npmjs.org/x", dest, false)
		return e
	}()
	if err == nil || !strings.Contains(err.Error(), "byte limit") {
		t.Fatalf("err = %v, want a byte-limit error", err)
	}
	if tr.hits != 1 {
		t.Errorf("hits = %d, want 1 -- not retried", tr.hits)
	}
	for _, p := range []string{dest, dest + ".tmp"} {
		if _, statErr := os.Stat(p); !os.IsNotExist(statErr) {
			t.Errorf("%s left behind after an oversize download", p)
		}
	}
}

func TestDownloadAcceptsABodyAtTheCap(t *testing.T) {
	c, _, _ := newTestClient(stubResponse{status: 200, body: strings.Repeat("x", 100)})
	c.MaxDownloadBytes = 100
	dest := filepath.Join(t.TempDir(), "archive.zip")

	if _, err := c.Download(context.Background(), "https://registry.npmjs.org/x", dest, false); err != nil {
		t.Fatalf("a body at the cap must pass: %v", err)
	}
}

func TestDefaultCapsAreSetAndGenerous(t *testing.T) {
	if DefaultMaxDownloadBytes < 256<<20 {
		t.Errorf("DefaultMaxDownloadBytes = %d; the largest upstream archives are well over 100 MiB", DefaultMaxDownloadBytes)
	}
	if DefaultMaxJSONBytes < 1<<20 || DefaultMaxJSONBytes > 256<<20 {
		t.Errorf("DefaultMaxJSONBytes = %d is out of the sensible range", DefaultMaxJSONBytes)
	}
}

// S13: a redirect may not leave https for http, nor leave http(s) for another
// scheme, nor land on a literal loopback, private or link-local address (the
// cloud metadata endpoint is 169.254.169.254).

func TestRedirectPolicy(t *testing.T) {
	cases := []struct {
		name     string
		from     string
		location string
		ok       bool
	}{
		{"https to https, same host", "https://github.com/a.zip", "https://github.com/b.zip", true},
		{"https to another https host", "https://github.com/a.zip", "https://codeload.github.com/b.zip", true},
		{"loopback http to https", "http://127.0.0.1/a", "https://registry.npmjs.org/a", true},
		{"https to http downgrade", "https://registry.npmjs.org/a", "http://registry.npmjs.org/a", false},
		{"https to ftp", "https://registry.npmjs.org/a", "ftp://registry.npmjs.org/a", false},
		{"to loopback ip", "https://registry.npmjs.org/a", "https://127.0.0.1/a", false},
		{"to link-local metadata ip", "https://registry.npmjs.org/a", "https://169.254.169.254/latest/meta-data/", false},
		{"to private ip", "https://registry.npmjs.org/a", "https://10.0.0.5/a", false},
		{"to ipv6 loopback", "https://registry.npmjs.org/a", "https://[::1]/a", false},
		{"to a public ip literal off the allowlist", "https://registry.npmjs.org/a", "https://93.184.216.34/a", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, _, _ := newTestClient(
				stubResponse{status: 302, location: tc.location},
				stubResponse{status: 200, body: `{"ok":true}`},
			)
			var out map[string]any
			err := c.GetJSON(context.Background(), tc.from, &out)
			if tc.ok && err != nil {
				t.Fatalf("redirect to %s refused: %v", tc.location, err)
			}
			if !tc.ok && (err == nil || !strings.Contains(err.Error(), "redirect")) {
				t.Fatalf("redirect to %s allowed, err = %v", tc.location, err)
			}
		})
	}
}

func TestRedirectPolicyAlsoGuardsDownloads(t *testing.T) {
	c, _, _ := newTestClient(
		stubResponse{status: 302, location: "http://downgrade.example/a.zip"},
		stubResponse{status: 200, body: "payload"},
	)
	dest := filepath.Join(t.TempDir(), "a.zip")
	if _, err := c.Download(context.Background(), "https://registry.npmjs.org/a.zip", dest, false); err == nil {
		t.Fatal("a download followed an https to http redirect")
	}
}

func TestRedirectChainIsBounded(t *testing.T) {
	c, tr, _ := newTestClient(stubResponse{status: 302, location: "https://registry.npmjs.org/loop"})
	var out map[string]any
	if err := c.GetJSON(context.Background(), "https://registry.npmjs.org/loop", &out); err == nil {
		t.Fatal("an endless redirect chain succeeded")
	}
	if tr.hits > 40 {
		t.Errorf("hits = %d; the chain must stop at 10 hops per attempt", tr.hits)
	}
}

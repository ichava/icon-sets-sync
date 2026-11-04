// Package httpx is the HTTP layer: a shared client, retrying JSON reads and
// resumable-by-existence downloads.
//
// The retry policy is ported from core/http.py's tenacity decorators rather
// than re-chosen. GitHub's API is polled unauthenticated here, so it is
// rate-limited, and loosening the policy would fail a sync that used to work.
package httpx

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	// UserAgent identifies the toolkit to the registries it polls.
	UserAgent = "icon-sets-sync (+https://github.com/ichava/icon-sets-sync)"

	// DefaultTimeout and DefaultDownloadTimeout match core/http.py.
	DefaultTimeout         = 30 * time.Second
	DefaultDownloadTimeout = 300 * time.Second

	// Attempts and the backoff bounds are tenacity's:
	// stop_after_attempt(4), wait_exponential(multiplier=2, min=2, max=30)
	// for JSON, and max=60 for downloads.
	Attempts       = 4
	backoffFactor  = 2 * time.Second
	backoffMinWait = 2 * time.Second
	backoffMaxJSON = 30 * time.Second
	backoffMaxFile = 60 * time.Second

	// DefaultMaxJSONBytes caps a version-discovery or registry response. The
	// largest real one is a few hundred KiB; 32 MiB is two orders above that.
	DefaultMaxJSONBytes int64 = 32 << 20

	// DefaultMaxDownloadBytes caps one archive download (S6). The largest
	// upstream archives are a few hundred MiB; 1 GiB leaves headroom without
	// letting a compromised upstream fill a runner's disk.
	DefaultMaxDownloadBytes int64 = 1 << 30

	// maxRedirects is net/http's own default, made explicit because the
	// redirect policy below replaces the function that enforced it.
	maxRedirects = 10
)

// errOverLimit marks a body that exceeded its cap. It is permanent: a retry
// would read the same oversize body, so it is never retried.
type errOverLimit struct {
	url   string
	limit int64
}

// errRedirectRefused marks a redirect the policy below rejected. Like an
// oversize body it is permanent, so a retry loop must not swallow it.
type errRedirectRefused struct{ reason string }

func (e *errRedirectRefused) Error() string { return "redirect: " + e.reason }

func (e *errOverLimit) Error() string {
	return fmt.Sprintf("GET %s: response is over the %d byte limit", e.url, e.limit)
}

// Client performs the toolkit's HTTP requests.
type Client struct {
	// Transport is the round tripper both the JSON and download clients are
	// built on. Tests supply a stub here rather than standing up an
	// httptest.Server: the sandbox this is developed in refuses to bind a
	// listening socket, and a suite that silently skips there would be the
	// same shape of false green this estate has been bitten by before.
	Transport http.RoundTripper

	// Sleep is swapped by tests so the retry path can be exercised without
	// waiting out the real backoff.
	Sleep func(time.Duration)

	// MaxJSONBytes and MaxDownloadBytes cap what one response body may hold.
	// Zero means the defaults below. Tests set them small.
	MaxJSONBytes     int64
	MaxDownloadBytes int64

	// AllowedHosts extends the built-in upstream allowlist for this client
	// only. The run-wide `allowed_hosts` setting reaches every client through
	// SetExtraAllowedHosts instead.
	AllowedHosts []string
}

// New returns a client with the shared timeout.
func New() *Client {
	return &Client{Sleep: time.Sleep}
}

func (c *Client) clientFor(timeout time.Duration) *http.Client {
	transport := c.Transport
	if transport == nil {
		transport = defaultTransport()
	}
	return &http.Client{Timeout: timeout, Transport: transport, CheckRedirect: c.checkRedirect}
}

// checkRedirect is the redirect policy (S13, P2-G5, P2-G6).
//
// Every hop is a fetch, so each is held to the same rules as the first request
// except the loopback exemption, which is for a test's own server and is never
// a place a real upstream sends a client. The legitimate chains stay inside the
// allowlist (github.com to codeload.github.com). What no legitimate upstream
// does is leave https for plain http, leave http(s) altogether, land on a host
// nobody listed, or hand a client to a loopback, private or link-local address
// (169.254.169.254 is the cloud metadata service). Those are refused, and the
// chain is bounded.
func (c *Client) checkRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= maxRedirects {
		return &errRedirectRefused{fmt.Sprintf("stopped after %d redirects", maxRedirects)}
	}

	scheme := strings.ToLower(req.URL.Scheme)
	if scheme != "http" && scheme != "https" {
		return &errRedirectRefused{fmt.Sprintf("refusing scheme %q", req.URL.Scheme)}
	}
	if prev := via[len(via)-1]; strings.EqualFold(prev.URL.Scheme, "https") && scheme == "http" {
		return &errRedirectRefused{fmt.Sprintf("refusing a downgrade from https to http (%s)", req.URL.Redacted())}
	}
	if scheme == "http" {
		return &errRedirectRefused{fmt.Sprintf("refusing %s: %s", req.URL.Redacted(), schemeRule)}
	}
	if ip := net.ParseIP(req.URL.Hostname()); ip != nil &&
		(ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
			ip.IsUnspecified() || ip.IsMulticast()) {
		return &errRedirectRefused{fmt.Sprintf("refusing the non-public address %s", ip)}
	}
	if err := c.checkHost(req.URL.Redacted(), normaliseHost(req.URL.Hostname())); err != nil {
		return &errRedirectRefused{err.Error()}
	}
	return nil
}

func (c *Client) maxJSON() int64 {
	if c.MaxJSONBytes > 0 {
		return c.MaxJSONBytes
	}
	return DefaultMaxJSONBytes
}

func (c *Client) maxDownload() int64 {
	if c.MaxDownloadBytes > 0 {
		return c.MaxDownloadBytes
	}
	return DefaultMaxDownloadBytes
}

var (
	transportOnce sync.Once
	transportVal  http.RoundTripper
)

// defaultTransport is http.DefaultTransport, with SSL_CERT_FILE loaded
// explicitly and the proxy preserved.
//
// The proxy half is the load-bearing one, and it is unconditional. The
// transport is *cloned* from http.DefaultTransport rather than built fresh,
// because a hand-rolled &http.Transport{TLSClientConfig: ...} silently drops
// Proxy: http.ProxyFromEnvironment along with the connection-pool and timeout
// defaults. Losing the proxy turns every request into a direct dial, which
// fails as "no such host" and reads like DNS rather than like the
// configuration mistake it is. Two sessions have now confirmed that
// independently; it is the part to keep if this function is ever simplified.
//
// The certificate half is defensive rather than established. Go is documented
// to honour SSL_CERT_FILE, and on Linux -- which is what the Docker image runs,
// and therefore what production uses -- it does. On the macOS machine this was
// developed on it demonstrably did not: with the variable set and visible to
// the process, x509.SystemCertPool() returned zero subjects and every TLS dial
// failed, while `gh` on the same toolchain and in the same shell succeeded
// (it loads the bundle itself). A second session measured the opposite on
// nominally the same setup and neither of us could reproduce the other.
//
// So this does not claim what Go does. It loads the bundle explicitly, which
// is correct under either reading: where Go already honours the variable this
// installs the same roots twice and changes nothing, and where it does not the
// tool still works behind a TLS-intercepting proxy. If someone later
// establishes the mechanism, the thing to fix is this comment -- not the code.
func defaultTransport() http.RoundTripper {
	transportOnce.Do(func() {
		transportVal = http.DefaultTransport

		path := os.Getenv("SSL_CERT_FILE")
		if path == "" {
			return
		}
		pem, err := os.ReadFile(path)
		if err != nil {
			return
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return
		}

		base, ok := http.DefaultTransport.(*http.Transport)
		if !ok {
			return
		}
		cloned := base.Clone()
		if cloned.TLSClientConfig == nil {
			cloned.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
		}
		cloned.TLSClientConfig.RootCAs = pool
		transportVal = cloned
	})
	return transportVal
}

func (c *Client) sleep(d time.Duration) {
	if c.Sleep != nil {
		c.Sleep(d)
	} else {
		time.Sleep(d)
	}
}

// backoff returns the wait before the given attempt, counting from 1.
func backoff(attempt int, max time.Duration) time.Duration {
	wait := time.Duration(math.Pow(2, float64(attempt-1))) * backoffFactor
	if wait < backoffMinWait {
		wait = backoffMinWait
	}
	if wait > max {
		wait = max
	}
	return wait
}

// retryable reports whether a status is worth another attempt.
//
// Python retried on any RequestException, and raise_for_status makes every
// non-2xx one of those -- so a 404 cost four attempts and three backoffs before
// reporting what it knew immediately. This narrows retries to transport
// errors, 429 and 5xx. A deliberate improvement, not a port slip.
func retryable(status int) bool {
	return status == http.StatusTooManyRequests || status >= 500
}

// GetJSON fetches and decodes a JSON document.
func (c *Client) GetJSON(ctx context.Context, url string, out any) error {
	if err := c.CheckURL(url); err != nil {
		return err
	}
	client := c.clientFor(DefaultTimeout)
	var lastErr error

	for attempt := 1; attempt <= Attempts; attempt++ {
		if attempt > 1 {
			c.sleep(backoff(attempt-1, backoffMaxJSON))
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return err
		}
		req.Header.Set("User-Agent", UserAgent)
		req.Header.Set("Accept", "application/json")

		resp, err := client.Do(req)
		if err != nil {
			var refused *errRedirectRefused
			if errors.As(err, &refused) {
				return fmt.Errorf("GET %s: %w", url, err)
			}
			lastErr = err
			continue
		}

		limit := c.maxJSON()
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, limit+1))
		_ = resp.Body.Close()
		if int64(len(body)) > limit {
			return &errOverLimit{url: url, limit: limit}
		}

		if resp.StatusCode >= 400 {
			lastErr = fmt.Errorf("GET %s: %s", url, resp.Status)
			if retryable(resp.StatusCode) {
				continue
			}
			return lastErr
		}
		if readErr != nil {
			lastErr = readErr
			continue
		}
		if err := json.Unmarshal(body, out); err != nil {
			// A malformed body will not repair itself on a retry.
			return fmt.Errorf("GET %s: decoding response: %w", url, err)
		}
		return nil
	}

	return fmt.Errorf("GET %s: after %d attempts: %w", url, Attempts, lastErr)
}

// Download fetches url to dest and returns dest.
//
// Idempotent by existence: a non-empty file already at dest is kept unless
// force is set. The write goes to a sibling .tmp and is renamed, so an
// interrupted download never leaves a truncated file that the next run would
// treat as a complete cache hit.
func (c *Client) Download(ctx context.Context, url, dest string, force bool) (string, error) {
	if err := c.CheckURL(url); err != nil {
		return "", err
	}
	if !force {
		if info, err := os.Stat(dest); err == nil && info.Size() > 0 {
			return dest, nil
		}
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return "", err
	}

	client := c.clientFor(DefaultDownloadTimeout)
	var lastErr error

	for attempt := 1; attempt <= Attempts; attempt++ {
		if attempt > 1 {
			c.sleep(backoff(attempt-1, backoffMaxFile))
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return "", err
		}
		req.Header.Set("User-Agent", UserAgent)

		resp, err := client.Do(req)
		if err != nil {
			var refused *errRedirectRefused
			if errors.As(err, &refused) {
				return "", fmt.Errorf("GET %s: %w", url, err)
			}
			lastErr = err
			continue
		}
		if resp.StatusCode >= 400 {
			_ = resp.Body.Close()
			lastErr = fmt.Errorf("GET %s: %s", url, resp.Status)
			if retryable(resp.StatusCode) {
				continue
			}
			return "", lastErr
		}

		tmp := dest + ".tmp"
		f, err := os.Create(tmp)
		if err != nil {
			_ = resp.Body.Close()
			return "", err
		}
		limit := c.maxDownload()
		n, copyErr := io.Copy(f, io.LimitReader(resp.Body, limit+1))
		closeErr := f.Close()
		_ = resp.Body.Close()

		if copyErr == nil && n > limit {
			_ = os.Remove(tmp)
			return "", &errOverLimit{url: url, limit: limit}
		}

		if copyErr != nil || closeErr != nil {
			_ = os.Remove(tmp)
			if lastErr = copyErr; lastErr == nil {
				lastErr = closeErr
			}
			continue
		}
		if err := os.Rename(tmp, dest); err != nil {
			return "", err
		}
		return dest, nil
	}

	return "", fmt.Errorf("GET %s: after %d attempts: %w", url, Attempts, lastErr)
}

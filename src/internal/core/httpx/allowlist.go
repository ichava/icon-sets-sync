package httpx

import (
	"fmt"
	"net"
	"net/url"
	"sort"
	"strings"
	"sync"
)

// defaultAllowedHosts is every host a shipped recipe fetches from through this
// client, and nothing else. Each entry is backed by a call site, and
// TestEveryShippedUpstreamIsAllowedByDefault (internal/core/recipes) fails if a
// recipe or pack config reaches a host that is missing here:
//
//	registry.npmjs.org   version_check_url of the npm-backed packs (tabler, flag, emoji)
//	api.github.com       version_check_url of icon-sets-openmoji
//	github.com           the OpenMoji tag archive; its 302 goes to codeload
//	codeload.github.com  where github.com's archive redirect lands
//	unicode.org          the CLDR emoji-test.txt the emoji categoriser reads
//
// objects.githubusercontent.com and release-assets.githubusercontent.com serve
// release assets. No recipe downloads one (every GitHub source fetches the tag
// archive), so they are not here; allowed_hosts adds them for a pack that does.
var defaultAllowedHosts = []string{
	"api.github.com",
	"codeload.github.com",
	"github.com",
	"registry.npmjs.org",
	"unicode.org",
}

// DefaultAllowedHosts returns the built-in allowlist, sorted. It is a copy.
func DefaultAllowedHosts() []string {
	return append([]string(nil), defaultAllowedHosts...)
}

var (
	extraMu    sync.RWMutex
	extraHosts []string
)

// SetExtraAllowedHosts sets the hosts every client allows beyond the defaults.
// It is the run-wide `allowed_hosts` setting, applied once at startup the way
// the private config directory is. Passing nil clears it.
func SetExtraAllowedHosts(hosts []string) {
	clean := make([]string, 0, len(hosts))
	for _, h := range hosts {
		if h = normaliseHost(h); h != "" {
			clean = append(clean, h)
		}
	}
	extraMu.Lock()
	extraHosts = clean
	extraMu.Unlock()
}

// ParseHostList splits the `allowed_hosts` setting (comma or space separated)
// into lowercase, de-duplicated host names.
func ParseHostList(raw string) []string {
	var out []string
	seen := map[string]bool{}
	for _, f := range strings.FieldsFunc(raw, func(r rune) bool { return r == ',' || r == ' ' || r == '\t' || r == '\n' }) {
		if h := normaliseHost(f); h != "" && !seen[h] {
			seen[h] = true
			out = append(out, h)
		}
	}
	return out
}

func normaliseHost(h string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(h)), ".")
}

// allowedSet is the defaults, the run-wide extras and the client's own.
func (c *Client) allowedSet() map[string]bool {
	set := map[string]bool{}
	for _, h := range defaultAllowedHosts {
		set[h] = true
	}
	extraMu.RLock()
	for _, h := range extraHosts {
		set[h] = true
	}
	extraMu.RUnlock()
	for _, h := range c.AllowedHosts {
		if h = normaliseHost(h); h != "" {
			set[h] = true
		}
	}
	return set
}

// isLoopbackHost is the exemption for tests that stand up an httptest server:
// exactly localhost, 127.0.0.1 and ::1. 127.0.0.2 and its kin are not exempt.
func isLoopbackHost(host string) bool {
	host = normaliseHost(host)
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && (ip.Equal(net.IPv4(127, 0, 0, 1)) || ip.Equal(net.IPv6loopback))
}

const schemeRule = "upstream URLs must use https (only http://127.0.0.1, http://localhost and http://[::1] are exempt)"

// CheckScheme enforces the https-only rule (P2-G6) on a configured URL. It
// knows nothing about hosts, so it is safe to run when a config loads, before
// the run-wide allowlist is in place.
func CheckScheme(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("%s: not a valid URL: %v", raw, err)
	}
	if strings.EqualFold(u.Scheme, "https") {
		return nil
	}
	if strings.EqualFold(u.Scheme, "http") && isLoopbackHost(u.Hostname()) {
		return nil
	}
	return fmt.Errorf("refusing %s: %s", raw, schemeRule)
}

// CheckURL is the rule for the first request of a fetch: https (or http to
// loopback), and a host on the allowlist (or loopback). GetJSON and Download
// run it before sending anything; it is exported so a test can ask the same
// question about a URL without fetching it.
func (c *Client) CheckURL(raw string) error {
	if err := CheckScheme(raw); err != nil {
		return err
	}
	u, _ := url.Parse(raw)
	host := normaliseHost(u.Hostname())
	if isLoopbackHost(host) {
		return nil
	}
	return c.checkHost(raw, host)
}

func (c *Client) checkHost(raw, host string) error {
	set := c.allowedSet()
	if set[host] {
		return nil
	}
	names := make([]string, 0, len(set))
	for h := range set {
		names = append(names, h)
	}
	sort.Strings(names)
	return fmt.Errorf("refusing %s: host %q is not on the upstream allowlist (%s); "+
		"to allow it, set allowed_hosts (ICHAVA_ALLOWED_HOSTS=%s, or --set allowed_hosts=%s)",
		raw, host, strings.Join(names, ", "), host, host)
}

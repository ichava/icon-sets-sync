package cli

import (
	"testing"

	"github.com/ichava/icon-sets-sync/src/internal/core/httpx"
)

// allowed_hosts reaches the HTTP client from every layer a run-wide setting
// can come from, and is cleared again by the next invocation, so one run never
// inherits another's allowlist.
func TestAllowedHostsReachTheHTTPClient(t *testing.T) {
	const url = "https://mirror.example/x"
	allowed := func() bool { return httpx.New().CheckURL(url) == nil }
	t.Cleanup(func() { httpx.SetExtraAllowedHosts(nil) })

	isolate(t)
	if _, code := run(t, "--config-dir", shippedConfig, "list-packs"); code != ExitOK || allowed() {
		t.Fatalf("exit %d, allowed=%v: a host is allowed with no setting", code, allowed())
	}

	if _, code := run(t, "--config-dir", shippedConfig, "--set", "allowed_hosts=mirror.example", "list-packs"); code != ExitOK || !allowed() {
		t.Fatalf("--set allowed_hosts: exit %d, allowed=%v", code, allowed())
	}

	if _, code := run(t, "--config-dir", shippedConfig, "list-packs"); code != ExitOK || allowed() {
		t.Fatalf("the next invocation inherited the allowlist: exit %d, allowed=%v", code, allowed())
	}

	t.Setenv("ICHAVA_ALLOWED_HOSTS", "other.example, mirror.example")
	if _, code := run(t, "--config-dir", shippedConfig, "list-packs"); code != ExitOK || !allowed() {
		t.Fatalf("ICHAVA_ALLOWED_HOSTS: exit %d, allowed=%v", code, allowed())
	}
}

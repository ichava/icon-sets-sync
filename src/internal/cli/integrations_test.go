package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ichava/icon-sets-sync/src/internal/core/settings"
	"github.com/ichava/icon-sets-sync/src/internal/mcpserver"
)

// isolateState is isolate plus a private state directory.
func isolateState(t *testing.T) string {
	t.Helper()
	isolate(t)
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)
	return filepath.Join(dir, "icon-sets-sync")
}

// Guard: a fresh config turns on no integration. Proven red by flipping
// api.enabled's default to true: the API, and this test, came up.
func TestEveryIntegrationIsOffOnAFreshConfig(t *testing.T) {
	run := settings.Resolve()
	checked := 0
	for _, k := range settings.Keys {
		if k.Group != settings.GroupIntegrations {
			continue
		}
		checked++
		switch k.Kind {
		case settings.Bool:
			if run.Bool(k.Name) {
				t.Errorf("%s defaults to on", k.Name)
			}
		case settings.Webhooks:
			if len(run.Webhooks(k.Name)) != 0 {
				t.Errorf("%s has default endpoints", k.Name)
			}
		}
	}
	if checked < 12 {
		t.Fatalf("checked %d integration keys; the group lost some", checked)
	}
	in, err := newIntegrations(t.TempDir(), "127.0.0.1", run)
	if err != nil {
		t.Fatal(err)
	}
	if in.api.Enabled || in.dispatch != nil || in.sub != nil {
		t.Fatalf("a fresh config started integrations: %+v", in)
	}
	plan, err := planTLS("127.0.0.1", run)
	if err != nil || plan.mode != tlsNone {
		t.Fatalf("loopback plan %+v, %v", plan, err)
	}
}

// Guard: a bind beyond loopback defaults to TLS. Proven red by returning
// tlsNone for every host: the remote cases below failed.
func TestANonLoopbackBindDefaultsToTLS(t *testing.T) {
	fresh := settings.Resolve()
	for _, host := range []string{"0.0.0.0", "::", "192.168.1.20", "build.example.com"} {
		plan, err := planTLS(host, fresh)
		if err != nil || plan.mode != tlsSelfSigned {
			t.Errorf("%s: %+v, %v; want a self-signed certificate", host, plan, err)
		}
	}
	for _, host := range []string{"127.0.0.1", "localhost", "::1"} {
		if plan, _ := planTLS(host, fresh); plan.mode != tlsNone {
			t.Errorf("%s: %+v; loopback stays plain HTTP", host, plan)
		}
	}
	own := settings.Resolve(settings.LayerValues{Layer: settings.LayerGlobal, Values: settings.Values{
		"api.tls_cert": "/etc/iss/cert.pem", "api.tls_key": "/etc/iss/key.pem"}})
	if plan, _ := planTLS("0.0.0.0", own); plan.mode != tlsOwn {
		t.Errorf("with a certificate: %+v", plan)
	}
	proxy := settings.Resolve(settings.LayerValues{Layer: settings.LayerGlobal, Values: settings.Values{"api.tls": settings.TLSOffBehindProxy}})
	if plan, _ := planTLS("0.0.0.0", proxy); plan.mode != tlsProxy {
		t.Errorf("off-behind-proxy: %+v", plan)
	}
	half := settings.Resolve(settings.LayerValues{Layer: settings.LayerGlobal, Values: settings.Values{"api.tls_cert": "/etc/iss/cert.pem"}})
	if _, err := planTLS("0.0.0.0", half); err == nil {
		t.Error("a certificate without its key must be refused")
	}
}

// The API stays off on a remote bind without api.allow_remote.
func TestRemoteAPINeedsAllowRemote(t *testing.T) {
	on := settings.Values{"api.enabled": true}
	in, err := newIntegrations(t.TempDir(), "0.0.0.0", settings.Resolve(settings.LayerValues{Layer: settings.LayerGlobal, Values: on}))
	if err != nil || in.api.Enabled || in.apiNote == "" {
		t.Fatalf("remote without allow_remote: %+v, %v", in.api, err)
	}
	on["api.allow_remote"] = true
	in, err = newIntegrations(t.TempDir(), "0.0.0.0", settings.Resolve(settings.LayerValues{Layer: settings.LayerGlobal, Values: on}))
	if err != nil || !in.api.Enabled {
		t.Fatalf("remote with allow_remote: %+v, %v", in.api, err)
	}
}

var tokenLine = regexp.MustCompile(`(?m)^iss_[A-Za-z0-9_-]{43}$`)

func TestAPITokenCreateListRevoke(t *testing.T) {
	dir := isolateState(t)
	out, code := run(t, "api", "token", "create", "--name", "ci", "--scope", "read,sync", "--expires", "30d")
	if code != ExitOK || !tokenLine.MatchString(out) || !strings.Contains(out, "shown once") {
		t.Fatalf("create: exit %d\n%s", code, out)
	}
	secret := tokenLine.FindString(out)
	data, err := os.ReadFile(filepath.Join(dir, "api-tokens.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), secret) {
		t.Fatal("the token itself reached the store; only its hash may")
	}
	if info, _ := os.Stat(filepath.Join(dir, "api-tokens.json")); info.Mode().Perm() != 0o600 {
		t.Fatalf("store mode %v", info.Mode().Perm())
	}
	out, code = run(t, "api", "token", "list")
	if code != ExitOK || !strings.Contains(out, "ci") || !strings.Contains(out, "read, sync") || strings.Contains(out, secret) {
		t.Fatalf("list: exit %d\n%s", code, out)
	}
	if out, code := run(t, "api", "token", "create", "--name", "ci2", "--scope", "write"); code != ExitUsage {
		t.Fatalf("an unknown scope: exit %d\n%s", code, out)
	}
	if out, code := run(t, "api", "token", "revoke", "ci"); code != ExitOK {
		t.Fatalf("revoke: exit %d\n%s", code, out)
	}
	out, _ = run(t, "api", "token", "list")
	if !strings.Contains(out, "revoked") {
		t.Fatalf("list after revoke:\n%s", out)
	}
}

var secretLine = regexp.MustCompile(`(?m)^whsec_[A-Za-z0-9+/]{43}=$`)

func TestWebhooksAddListRotateRemove(t *testing.T) {
	dir := isolateState(t)
	cfg := filepath.Join("..", "..", "..", "config")
	out, code := run(t, "--config-dir", cfg, "webhooks", "add", "ci", "--url", "http://ci.example.com/hook", "--event", "job.finished")
	if code != ExitUsage || !strings.Contains(out, "https") {
		t.Fatalf("plain http to a remote host: exit %d\n%s", code, out)
	}
	out, code = run(t, "--config-dir", cfg, "webhooks", "add", "ci", "--url", "https://ci.example.com/hook", "--event", "job.progress")
	if code != ExitUsage || !strings.Contains(out, "stream only") {
		t.Fatalf("a stream-only type: exit %d\n%s", code, out)
	}
	out, code = run(t, "--config-dir", cfg, "webhooks", "add", "ci", "--url", "https://ci.example.com/hook", "--event", "job.finished", "--event", "pack.synced")
	if code != ExitOK || !secretLine.MatchString(out) {
		t.Fatalf("add: exit %d\n%s", code, out)
	}
	secret := secretLine.FindString(out)
	global, err := os.ReadFile(filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "icon-sets-sync", "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(global), "whsec_") || !strings.Contains(string(global), "https://ci.example.com/hook") {
		t.Fatalf("global config:\n%s", global)
	}
	stored, err := os.ReadFile(filepath.Join(dir, "webhook-secrets.json"))
	if err != nil || !strings.Contains(string(stored), secret) {
		t.Fatalf("secrets file: %v", err)
	}
	if info, _ := os.Stat(filepath.Join(dir, "webhook-secrets.json")); info.Mode().Perm() != 0o600 {
		t.Fatalf("secrets mode %v", info.Mode().Perm())
	}
	out, code = run(t, "--config-dir", cfg, "--json", "webhooks", "list")
	var listed struct {
		Endpoints []struct {
			Name, Secret string
			Events       []string
		}
	}
	if code != ExitOK || json.Unmarshal([]byte(out), &listed) != nil || len(listed.Endpoints) != 1 || listed.Endpoints[0].Secret != "generated" {
		t.Fatalf("list: exit %d\n%s", code, out)
	}
	out, code = run(t, "--config-dir", cfg, "webhooks", "rotate", "ci")
	if code != ExitOK || !secretLine.MatchString(out) || secretLine.FindString(out) == secret {
		t.Fatalf("rotate: exit %d\n%s", code, out)
	}
	if out, code := run(t, "--config-dir", cfg, "webhooks", "remove", "ci"); code != ExitOK {
		t.Fatalf("remove: exit %d\n%s", code, out)
	}
	stored, _ = os.ReadFile(filepath.Join(dir, "webhook-secrets.json"))
	if strings.Contains(string(stored), `"ci"`) {
		t.Fatal("remove left the secret")
	}
}

// The test command goes through the guarded sender: a loopback receiver is
// refused at connect time with no network involved.
func TestWebhooksTestRefusesAPrivateAddress(t *testing.T) {
	isolateState(t)
	cfg := filepath.Join("..", "..", "..", "config")
	if out, code := run(t, "--config-dir", cfg, "webhooks", "add", "lan", "--url", "https://127.0.0.1:9/hook", "--event", "job.finished"); code != ExitOK {
		t.Fatalf("add: exit %d\n%s", code, out)
	}
	out, code := run(t, "--config-dir", cfg, "webhooks", "test", "lan")
	if code != ExitFailure || !strings.Contains(out, "not allowed") {
		t.Fatalf("test: exit %d\n%s", code, out)
	}
}

func TestMCPIsOffUnlessEnabled(t *testing.T) {
	isolateState(t)
	out, code := run(t, "mcp")
	if code != ExitUsage || !strings.Contains(out, "mcp.enabled") {
		t.Fatalf("exit %d\n%s", code, out)
	}
}

// The mcp command serves the SDK's protocol; without mcp.allow_writes it
// offers the read tools only.
func TestMCPCommandServesTheReadTools(t *testing.T) {
	isolateState(t)
	serverSide, clientSide := mcp.NewInMemoryTransports()
	prev := mcpTransport
	mcpTransport = func() mcp.Transport { return serverSide }
	t.Cleanup(func() { mcpTransport = prev })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		root := NewRootCommand()
		root.SetOut(&strings.Builder{})
		root.SetErr(&strings.Builder{})
		root.SetArgs([]string{"--config-dir", filepath.Join("..", "..", "..", "config"), "mcp", "--enable"})
		_ = root.ExecuteContext(ctx)
	}()
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil)
	session, err := client.Connect(ctx, clientSide, nil)
	if err != nil {
		t.Fatal(err)
	}
	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tool := range tools.Tools {
		names = append(names, tool.Name)
	}
	slices.Sort(names)
	want := slices.Clone(mcpserver.ReadTools)
	slices.Sort(want)
	if !slices.Equal(names, want) {
		t.Fatalf("tools %v, want %v", names, want)
	}
	_ = session.Close()
	cancel()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("mcp did not stop")
	}
}

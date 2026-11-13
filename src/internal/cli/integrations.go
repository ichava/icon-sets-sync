package cli

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"time"

	"github.com/ichava/icon-sets-sync/src/internal/core/apitoken"
	"github.com/ichava/icon-sets-sync/src/internal/core/jsonlog"
	"github.com/ichava/icon-sets-sync/src/internal/core/settings"
	"github.com/ichava/icon-sets-sync/src/internal/core/tlscert"
	"github.com/ichava/icon-sets-sync/src/internal/events"
	"github.com/ichava/icon-sets-sync/src/internal/web"
	"github.com/ichava/icon-sets-sync/src/internal/webhooks"
)

// TLS modes serve chooses between.
const (
	tlsNone       = "none"        // a loopback bind: nothing leaves the machine
	tlsOwn        = "own"         // api.tls_cert and api.tls_key
	tlsSelfSigned = "self-signed" // generated once, kept in the state directory
	tlsProxy      = "off-behind-proxy"
)

// tlsPlan is how serve secures its listener.
type tlsPlan struct {
	mode      string
	cert, key string
}

// planTLS decides how serve terminates TLS. The secure choice is the
// default: any bind beyond loopback gets TLS, and plain HTTP there needs
// api.tls=off-behind-proxy said in so many words.
func planTLS(host string, run settings.Resolved) (tlsPlan, error) {
	cert, key := run.String("api.tls_cert"), run.String("api.tls_key")
	if (cert == "") != (key == "") {
		return tlsPlan{}, fmt.Errorf("api.tls_cert and api.tls_key are set together; only one is set")
	}
	if web.IsLoopback(host) {
		return tlsPlan{mode: tlsNone}, nil
	}
	switch {
	case run.String("api.tls") == settings.TLSOffBehindProxy:
		return tlsPlan{mode: tlsProxy}, nil
	case cert != "":
		return tlsPlan{mode: tlsOwn, cert: cert, key: key}, nil
	}
	return tlsPlan{mode: tlsSelfSigned}, nil
}

// load reads or makes the certificate the plan needs.
func (p tlsPlan) load(stateDir, host string, now time.Time) (tlscert.Result, error) {
	switch p.mode {
	case tlsOwn:
		return tlscert.Load(p.cert, p.key)
	case tlsSelfSigned:
		if stateDir == "" {
			return tlscert.Result{}, fmt.Errorf("no state directory for the self-signed certificate; set XDG_STATE_HOME, or api.tls_cert and api.tls_key")
		}
		return tlscert.SelfSigned(stateDir, host, now)
	}
	return tlscert.Result{}, nil
}

// describe prints what the plan did, so the person who started a remote
// server knows how a client should trust it.
func (p tlsPlan) describe(out, errOut io.Writer, r tlscert.Result) {
	switch p.mode {
	case tlsOwn:
		_, _ = fmt.Fprintf(out, "TLS: %s, SHA-256 fingerprint %s\n", r.CertPath, r.Fingerprint)
	case tlsSelfSigned:
		made := "kept"
		if r.New {
			made = "generated"
		}
		_, _ = fmt.Fprintf(out, "TLS: self-signed certificate %s (%s), SHA-256 fingerprint %s\n", r.CertPath, made, r.Fingerprint)
		_, _ = fmt.Fprintf(out, "  pin it: curl --cacert %s, or --pinnedpubkey 'sha256//%s'\n", r.CertPath, r.PublicKeyPin)
	case tlsProxy:
		_, _ = fmt.Fprintln(errOut, "warning: api.tls=off-behind-proxy serves plain HTTP beyond loopback; a TLS proxy must be in front of it")
	}
}

// integrations are the API, events and webhooks a serve run turns on.
type integrations struct {
	bus      *events.Bus
	api      web.APIOptions
	dispatch *webhooks.Dispatcher
	sub      *events.Subscription
	// apiNote says why the API is off on a remote bind.
	apiNote string
}

// newIntegrations reads the integration settings. Everything is off unless
// its setting is on; a remote bind also needs api.allow_remote for the API.
func newIntegrations(stateDir, host string, run settings.Resolved) (*integrations, error) {
	in := &integrations{bus: events.NewBus(events.Source(), nil)}
	if run.Bool("api.enabled") {
		switch {
		case !web.IsLoopback(host) && !run.Bool("api.allow_remote"):
			in.apiNote = "api: off on this remote bind; set api.allow_remote to serve it beyond loopback"
		case stateDir == "":
			return nil, fmt.Errorf("api.enabled needs a state directory for its tokens; set XDG_STATE_HOME")
		default:
			in.api = web.APIOptions{
				Enabled:      true,
				Tokens:       &apitoken.Store{Path: apitoken.DefaultPath(stateDir)},
				RateLimit:    run.Int("api.rate_limit"),
				EventsStream: run.Bool("events.stream"),
				Audit:        &jsonlog.Log{Path: filepath.Join(stateDir, "api-audit.jsonl"), Keep: 3},
			}
		}
	}
	if run.Bool("webhooks.enabled") {
		if stateDir == "" {
			return nil, fmt.Errorf("webhooks.enabled needs a state directory for its secrets; set XDG_STATE_HOME")
		}
		endpoints := run.Webhooks("webhooks.endpoints")
		in.dispatch = webhooks.NewDispatcher(stateDir, endpoints, newSender(stateDir, run), in.bus)
		in.api.Webhooks, in.api.WebhookEndpoints = in.dispatch, endpoints
		// Subscribed now, so server.started is delivered too.
		in.sub = in.bus.Subscribe(events.DefaultBuffer)
	}
	return in, nil
}

func newSender(stateDir string, run settings.Resolved) *webhooks.Sender {
	return &webhooks.Sender{
		Policy: webhooks.Policy{
			AllowPrivate:           run.Bool("webhooks.allow_private"),
			AllowInsecureLocalhost: run.Bool("webhooks.allow_insecure_localhost"),
		},
		Secrets: &webhooks.Secrets{Path: webhooks.DefaultSecretsPath(stateDir)},
	}
}

// start runs the webhook dispatcher until ctx ends.
func (in *integrations) start(ctx context.Context) {
	if in.dispatch != nil {
		go in.dispatch.Run(ctx, in.sub)
	}
}

func (in *integrations) describe(out io.Writer, origin string) {
	switch {
	case in.api.Enabled:
		extra := ""
		if in.api.EventsStream {
			extra = ", event stream at /api/v1/events"
		}
		_, _ = fmt.Fprintf(out, "api: %s/api/v1 for bearer tokens (`icon-sets-sync api token create`)%s\n", origin, extra)
	case in.apiNote != "":
		_, _ = fmt.Fprintln(out, in.apiNote)
	}
	if in.dispatch != nil {
		_, _ = fmt.Fprintf(out, "webhooks: delivering to %d endpoint(s)\n", len(in.dispatch.Endpoints))
	}
}

package webhooks

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/ichava/icon-sets-sync/src/internal/core/settings"
	"github.com/ichava/icon-sets-sync/src/internal/events"
	"github.com/ichava/icon-sets-sync/src/internal/version"
)

// Limits on one delivery.
const (
	// Timeout bounds a delivery from dial to the last response byte.
	Timeout = 10 * time.Second
	// MaxPayload is the largest body sent; a bigger event is not sent.
	MaxPayload = 256 << 10
	// maxResponse is how much of a receiver's answer is read.
	maxResponse = 64 << 10
)

// ErrTooLarge is an event whose body exceeds MaxPayload.
var ErrTooLarge = errors.New("webhook: payload over 256 KiB, not sent")

// Sender makes one signed delivery.
type Sender struct {
	Policy  Policy
	Secrets *Secrets
	// Now is the clock; nil means time.Now.
	Now func() time.Time

	// transport replaces the guarded transport; set by tests only, which
	// answer in memory because the sandbox cannot bind a port.
	transport http.RoundTripper
}

func (s *Sender) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// Result is how one attempt went.
type Result struct {
	// Status is the receiver's HTTP status; zero when none was received.
	Status int
	// Err is why it failed; nil for a 2xx.
	Err error
	// Permanent is set for a failure a retry cannot fix: a refused URL or
	// address, a missing secret, an oversized payload.
	Permanent bool
	Duration  time.Duration
}

// Send posts e to w, signed with every secret valid now.
func (s *Sender) Send(ctx context.Context, w settings.Webhook, e events.Event) Result {
	start := s.now()
	r := s.send(ctx, w, e)
	r.Duration = s.now().Sub(start)
	return r
}

func (s *Sender) send(ctx context.Context, w settings.Webhook, e events.Event) Result {
	loopbackOK, err := s.Policy.CheckURL(w.URL)
	if err != nil {
		return Result{Err: err, Permanent: true}
	}
	body, err := json.Marshal(e)
	if err != nil {
		return Result{Err: err, Permanent: true}
	}
	if len(body) > MaxPayload {
		return Result{Err: ErrTooLarge, Permanent: true}
	}
	secrets, err := s.Secrets.Signing(w)
	if err != nil {
		return Result{Err: err, Permanent: true}
	}
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.URL, bytes.NewReader(body))
	if err != nil {
		return Result{Err: err, Permanent: true}
	}
	ts := s.now()
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "icon-sets-sync/"+version.Version())
	req.Header.Set(HeaderID, e.ID)
	req.Header.Set(HeaderTimestamp, strconv.FormatInt(ts.Unix(), 10))
	req.Header.Set(HeaderSignature, SignatureHeader(secrets, e.ID, ts, body))

	resp, err := s.client(loopbackOK).Do(req)
	if err != nil {
		return Result{Err: err, Permanent: errors.Is(err, ErrDenied)}
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxResponse))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return Result{Status: resp.StatusCode, Err: fmt.Errorf("webhook: the receiver answered %s", resp.Status)}
	}
	return Result{Status: resp.StatusCode}
}

// client is a fresh client per delivery: no proxy (one would connect on our
// behalf, past the address check), no redirects (a 3xx is a failure, so a
// receiver cannot bounce a delivery to an internal address), a fixed
// timeout, and the guarded dialer.
func (s *Sender) client(loopbackOK bool) *http.Client {
	if s.transport != nil {
		return &http.Client{Timeout: Timeout, Transport: s.transport,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	return &http.Client{
		Timeout: Timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
		Transport: &http.Transport{
			Proxy:                  nil,
			DialContext:            s.Policy.DialContext(loopbackOK, Timeout),
			TLSHandshakeTimeout:    Timeout,
			ResponseHeaderTimeout:  Timeout,
			MaxResponseHeaderBytes: maxResponse,
			DisableKeepAlives:      true,
			ForceAttemptHTTP2:      true,
		},
	}
}

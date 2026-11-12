package web

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ichava/icon-sets-sync/src/internal/core/apitoken"
	"github.com/ichava/icon-sets-sync/src/internal/core/jsonlog"
	"github.com/ichava/icon-sets-sync/src/internal/core/settings"
	"github.com/ichava/icon-sets-sync/src/internal/webhooks"
)

// The REST API under /api/v1: off unless api.enabled, reached with a bearer
// token and nothing else, and no more able than the UI. See docs/tools/api.md.

// apiPrefix is every API path's start.
const apiPrefix = "/api/"

// MaxAPIBody is the largest request body the API reads.
const MaxAPIBody = 1 << 20

// APIOptions configures the REST API.
type APIOptions struct {
	// Enabled serves /api/v1 (api.enabled).
	Enabled bool
	// Tokens is the token store. Required when Enabled.
	Tokens *apitoken.Store
	// RateLimit is requests per minute per token (api.rate_limit); below 1
	// is 60.
	RateLimit int
	// EventsStream serves GET /api/v1/events (events.stream).
	EventsStream bool
	// Audit receives one record per write. Nil keeps none.
	Audit *jsonlog.Log
	// Webhooks is the delivery side, when webhooks.enabled; it answers the
	// test and deliveries endpoints. Nil means webhooks are off.
	Webhooks *webhooks.Dispatcher
	// WebhookEndpoints is webhooks.endpoints, for the test endpoint.
	WebhookEndpoints []settings.Webhook
}

// apiState is the API's per-process memory: rate-limit buckets and the
// Idempotency-Key replies.
type apiState struct {
	opts APIOptions

	mu      sync.Mutex
	buckets map[string]*bucket
	// idem maps token ID + Idempotency-Key to the job it started.
	idem map[string]idemEntry
}

type bucket struct {
	tokens float64
	last   time.Time
}

type idemEntry struct {
	jobID string
	at    time.Time
}

// idemTTL is how long an Idempotency-Key is remembered; maxIdem bounds how
// many are.
const (
	idemTTL = 24 * time.Hour
	maxIdem = 1000
)

func newAPIState(o APIOptions) apiState {
	if o.RateLimit < 1 {
		o.RateLimit = 60
	}
	return apiState{opts: o, buckets: map[string]*bucket{}, idem: map[string]idemEntry{}}
}

// allow takes one request from a token's bucket: RateLimit per minute, with
// a burst of the same size.
func (a *apiState) allow(id string, now time.Time) (bool, time.Duration) {
	a.mu.Lock()
	defer a.mu.Unlock()
	limit := float64(a.opts.RateLimit)
	b, ok := a.buckets[id]
	if !ok {
		b = &bucket{tokens: limit, last: now}
		a.buckets[id] = b
	}
	b.tokens += now.Sub(b.last).Minutes() * limit
	if b.tokens > limit {
		b.tokens = limit
	}
	b.last = now
	if b.tokens < 1 {
		wait := time.Duration((1 - b.tokens) / limit * float64(time.Minute))
		return false, wait
	}
	b.tokens--
	return true, 0
}

func (a *apiState) idemGet(key string, now time.Time) (string, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	e, ok := a.idem[key]
	if !ok || now.Sub(e.at) > idemTTL {
		return "", false
	}
	return e.jobID, true
}

func (a *apiState) idemPut(key, jobID string, now time.Time) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.idem) >= maxIdem {
		for k, e := range a.idem {
			if now.Sub(e.at) > idemTTL || len(a.idem) >= maxIdem {
				delete(a.idem, k)
			}
		}
	}
	a.idem[key] = idemEntry{jobID: jobID, at: now}
}

// Problem is an RFC 9457 problem details document.
type Problem struct {
	Type     string `json:"type"`
	Title    string `json:"title"`
	Status   int    `json:"status"`
	Detail   string `json:"detail,omitempty"`
	Instance string `json:"instance,omitempty"`
	// JobID names the run a 409 is about.
	JobID string `json:"job_id,omitempty"`
}

// problemType is a problem's type URI.
func problemType(slug string) string { return "urn:icon-sets-sync:problem:" + slug }

func writeProblem(w http.ResponseWriter, r *http.Request, status int, slug, title, detail string) {
	writeProblemDoc(w, Problem{Type: problemType(slug), Title: title, Status: status, Detail: detail, Instance: r.URL.Path})
}

func writeProblemDoc(w http.ResponseWriter, p Problem) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(p.Status)
	_ = json.NewEncoder(w).Encode(p)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

// apiHeaders are securityHeaders for JSON: no page may frame or embed it,
// and no CORS header is ever sent, so a browser on another origin cannot
// read an answer.
func apiHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Cross-Origin-Resource-Policy", "same-origin")
		next.ServeHTTP(w, r)
	})
}

// apiHostCheck is hostCheck for the API: the same allowlist, answered as a
// problem document.
func (s *Server) apiHostCheck(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.hostAllowed(r.Host) {
			writeProblem(w, r, http.StatusMisdirectedRequest, "unknown-host", "Unknown host",
				"This server only answers to the address it printed when it started.")
			return
		}
		next.ServeHTTP(w, r)
	})
}

type tokenKey struct{}

func tokenFrom(ctx context.Context) apitoken.Token {
	t, _ := ctx.Value(tokenKey{}).(apitoken.Token)
	return t
}

// apiAuth admits a request with a valid bearer token and nothing else: not
// the session cookie, not the launch token, and never a token in the URL,
// where logs and the history would keep it. It then applies the token's
// rate limit, caps the body, and audits every write.
func (s *Server) apiAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Has("token") || r.URL.Query().Has("access_token") {
			writeProblem(w, r, http.StatusBadRequest, "token-in-url", "Token in the URL",
				"Send the token in the Authorization header, never in the address.")
			return
		}
		scheme, presented, _ := strings.Cut(r.Header.Get("Authorization"), " ")
		if !strings.EqualFold(scheme, "Bearer") || strings.TrimSpace(presented) == "" {
			w.Header().Set("WWW-Authenticate", `Bearer realm="icon-sets-sync"`)
			writeProblem(w, r, http.StatusUnauthorized, "unauthorized", "Unauthorized",
				"Send Authorization: Bearer <token>; `icon-sets-sync api token create` makes one. The browser session does not open the API.")
			return
		}
		tok, err := s.opts.API.Tokens.Authenticate(strings.TrimSpace(presented))
		if err != nil {
			w.Header().Set("WWW-Authenticate", `Bearer realm="icon-sets-sync", error="invalid_token"`)
			detail := apitoken.ErrUnknown.Error()
			if errors.Is(err, apitoken.ErrExpired) || errors.Is(err, apitoken.ErrRevoked) {
				detail = err.Error()
			}
			writeProblem(w, r, http.StatusUnauthorized, "invalid-token", "Unauthorized", detail)
			return
		}
		if ok, wait := s.api.allow(tok.ID, s.now()); !ok {
			w.Header().Set("Retry-After", strconv.Itoa(int(wait.Seconds())+1))
			writeProblem(w, r, http.StatusTooManyRequests, "rate-limited", "Too many requests",
				"This token is over api.rate_limit requests a minute.")
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, MaxAPIBody)
		r = r.WithContext(context.WithValue(r.Context(), tokenKey{}, tok))
		if r.Method == http.MethodGet || r.Method == http.MethodHead {
			next.ServeHTTP(w, r)
			return
		}
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		s.audit(tok, r, rec.status)
	})
}

// statusRecorder remembers the status a handler wrote, for the audit.
type statusRecorder struct {
	http.ResponseWriter
	status int
	wrote  bool
}

func (r *statusRecorder) WriteHeader(code int) {
	if !r.wrote {
		r.status, r.wrote = code, true
	}
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	r.wrote = true
	return r.ResponseWriter.Write(b)
}

// AuditRecord is one line of api-audit.jsonl.
type AuditRecord struct {
	Time   time.Time `json:"time"`
	Token  string    `json:"token"`
	Method string    `json:"method"`
	Path   string    `json:"path"`
	Route  string    `json:"route,omitempty"`
	Status int       `json:"status"`
}

func (s *Server) audit(tok apitoken.Token, r *http.Request, status int) {
	rec := AuditRecord{Time: s.now().UTC(), Token: tok.Name, Method: r.Method, Path: r.URL.Path, Route: r.Pattern, Status: status}
	_ = s.opts.API.Audit.Append(rec)
}

// scoped wraps a handler with the scope it needs. A token without it gets
// 403, and the answer names the scope, since the holder already has the
// token.
func (s *Server) scoped(scope apitoken.Scope, h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !tokenFrom(r.Context()).Has(scope) {
			writeProblem(w, r, http.StatusForbidden, "insufficient-scope", "Forbidden",
				"This endpoint needs a token with the "+string(scope)+" scope.")
			return
		}
		h(w, r)
	}
}

// decodeBody reads a JSON body into v. An empty body leaves v alone.
func decodeBody(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil && !errors.Is(err, io.EOF) {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			writeProblem(w, r, http.StatusRequestEntityTooLarge, "body-too-large", "Body too large",
				"The API reads at most 1 MiB.")
			return false
		}
		writeProblem(w, r, http.StatusBadRequest, "bad-body", "Bad request body", err.Error())
		return false
	}
	return true
}

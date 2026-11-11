package web

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// The health check is how a second launch finds this one. It answers only to
// the launch token, in a header, so the token never sits in a URL it logs.
func TestHealthAnswersOnlyToTheToken(t *testing.T) {
	s, _ := newTestServer(t)
	for _, c := range []struct {
		name  string
		token string
		want  int
	}{
		{"right token", testToken, http.StatusOK},
		{"wrong token", "nope", http.StatusUnauthorized},
		{"no token", "", http.StatusUnauthorized},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "http://"+testHost+HealthPath, nil)
			if c.token != "" {
				r.Header.Set(HealthTokenHeader, c.token)
			}
			rec := do(s, r)
			if rec.Code != c.want {
				t.Fatalf("status %d, want %d", rec.Code, c.want)
			}
			if c.want == http.StatusOK && strings.TrimSpace(rec.Body.String()) != HealthBody {
				t.Errorf("body %q", rec.Body.String())
			}
			if strings.Contains(rec.Body.String(), testToken) {
				t.Error("the health response echoes the token")
			}
		})
	}
	// The Host check still applies.
	r := httptest.NewRequest(http.MethodGet, "http://evil.example"+HealthPath, nil)
	r.Header.Set(HealthTokenHeader, testToken)
	if rec := do(s, r); rec.Code == http.StatusOK {
		t.Fatal("answered a foreign Host")
	}
}

// Stop server is a state change: same-origin, with the session's CSRF token.
func TestStopServerNeedsCSRFAndSameOrigin(t *testing.T) {
	s, _ := newTestServer(t)
	c := signIn(t, s)

	r := httptest.NewRequest(http.MethodPost, "http://"+testHost+StopPath, strings.NewReader(url.Values{csrfField: {"stale"}}.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Origin", "http://"+testHost)
	r.AddCookie(c.cookie)
	if rec := do(s, r); rec.Code != http.StatusForbidden {
		t.Fatalf("a stale CSRF token: status %d", rec.Code)
	}
	r = httptest.NewRequest(http.MethodPost, "http://"+testHost+StopPath, strings.NewReader(url.Values{csrfField: {c.csrf}}.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Origin", "http://evil.example")
	r.AddCookie(c.cookie)
	if rec := do(s, r); rec.Code != http.StatusForbidden {
		t.Fatalf("a cross-origin post: status %d", rec.Code)
	}
	select {
	case <-s.Stopped():
		t.Fatal("a refused request stopped the server")
	default:
	}

	if rec := c.post(StopPath, nil); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Server stopped") {
		t.Fatalf("stop: status %d\n%s", rec.Code, rec.Body.String())
	}
	select {
	case <-s.Stopped():
	case <-time.After(time.Second):
		t.Fatal("the stop action did not signal a shutdown")
	}
}

// Every page carries the action, read-only included: stopping writes nothing.
func TestEveryPageOffersStopServer(t *testing.T) {
	for _, ro := range []bool{false, true} {
		s, _ := newTestServer(t, func(o *Options) { o.ReadOnly = ro })
		c := signIn(t, s)
		if body := c.get("/").Body.String(); !strings.Contains(body, `action="`+StopPath+`"`) {
			t.Fatalf("read-only=%v: no Stop server form on the dashboard", ro)
		}
		if rec := c.post(StopPath, nil); rec.Code != http.StatusOK {
			t.Fatalf("read-only=%v: stop answered %d", ro, rec.Code)
		}
	}
}

// Serve returns, gracefully, once the stop action has run.
func TestServeReturnsAfterStop(t *testing.T) {
	s, _ := newTestServer(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("cannot bind a loopback socket here: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- s.Serve(context.Background(), ln) }()
	s.Stop()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("Serve did not return after Stop")
	}
}

package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestEveryResponseCarriesTheSecurityHeaders(t *testing.T) {
	s, _ := newTestServer(t)
	c := signIn(t, s)
	for name, rec := range map[string]*httptest.ResponseRecorder{
		"page":         c.get("/settings"),
		"401":          do(s, httptest.NewRequest(http.MethodGet, "http://"+testHost+"/", nil)),
		"bad host":     do(s, httptest.NewRequest(http.MethodGet, "http://evil.example/", nil)),
		"404":          c.get("/nope"),
		"token swap":   do(s, httptest.NewRequest(http.MethodGet, "http://"+testHost+"/?token="+testToken, nil)),
		"asset":        c.get(mustAsset(t, s, "app.css")),
		"csrf refusal": func() *httptest.ResponseRecorder { return c.post("/check", url.Values{csrfField: {"x"}}) }(),
	} {
		t.Run(name, func(t *testing.T) {
			h := rec.Header()
			if h.Get("Content-Security-Policy") != ContentSecurityPolicy {
				t.Errorf("CSP %q", h.Get("Content-Security-Policy"))
			}
			if h.Get("X-Content-Type-Options") != "nosniff" || h.Get("Referrer-Policy") != "no-referrer" {
				t.Errorf("headers %v", h)
			}
		})
	}
}

func mustAsset(t *testing.T, s *Server, name string) string {
	t.Helper()
	u, err := s.assets.url(name)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func TestHostCheckRefusesARebindingHost(t *testing.T) {
	s, _ := newTestServer(t)
	c := signIn(t, s)
	for _, host := range []string{"evil.example", "evil.example:7878", "127.0.0.1:9999", "localhost.evil.example:7878"} {
		r := httptest.NewRequest(http.MethodGet, "http://"+testHost+"/", nil)
		r.Host = host
		r.AddCookie(c.cookie)
		if rec := do(s, r); rec.Code != http.StatusMisdirectedRequest {
			t.Errorf("Host %s: %d, want 421", host, rec.Code)
		}
	}
	for _, host := range []string{"127.0.0.1:7878", "localhost:7878", "[::1]:7878", "LOCALHOST:7878"} {
		r := httptest.NewRequest(http.MethodGet, "http://"+testHost+"/settings", nil)
		r.Host = host
		r.AddCookie(c.cookie)
		if rec := do(s, r); rec.Code != http.StatusOK {
			t.Errorf("Host %s: %d, want 200", host, rec.Code)
		}
	}
}

func TestAuth(t *testing.T) {
	s, _ := newTestServer(t)

	if rec := do(s, httptest.NewRequest(http.MethodGet, "http://"+testHost+"/", nil)); rec.Code != http.StatusUnauthorized ||
		!strings.Contains(rec.Body.String(), "printed when it started") {
		t.Errorf("no cookie: %d", rec.Code)
	}
	if rec := do(s, httptest.NewRequest(http.MethodGet, "http://"+testHost+"/?token=wrong", nil)); rec.Code != http.StatusUnauthorized {
		t.Errorf("wrong token: %d", rec.Code)
	}

	r := httptest.NewRequest(http.MethodGet, "http://"+testHost+"/settings?token="+testToken+"&theme=dark", nil)
	rec := do(s, r)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("token: %d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/settings?theme=dark" {
		t.Errorf("redirect %q still carries the token or lost the query", loc)
	}
	ck := rec.Result().Cookies()[0]
	if ck.Name != SessionCookie || !ck.HttpOnly || ck.SameSite != http.SameSiteStrictMode || ck.Path != "/" {
		t.Errorf("cookie %+v", ck)
	}
	if ck.Value == testToken {
		t.Error("the session cookie is the launch token")
	}

	forged := httptest.NewRequest(http.MethodGet, "http://"+testHost+"/", nil)
	forged.AddCookie(&http.Cookie{Name: SessionCookie, Value: "made-up"})
	if rec := do(s, forged); rec.Code != http.StatusUnauthorized {
		t.Errorf("unknown session: %d", rec.Code)
	}

	// A POST cannot exchange the token: that would let a cross-site form
	// carrying a leaked token sign a browser in.
	post := httptest.NewRequest(http.MethodPost, "http://"+testHost+"/check?token="+testToken, nil)
	if rec := do(s, post); rec.Code != http.StatusUnauthorized {
		t.Errorf("token on POST: %d", rec.Code)
	}
}

func TestStateChangesNeedSameOriginAndCSRF(t *testing.T) {
	s, _ := newTestServer(t)
	c := signIn(t, s)

	send := func(origin, fetchSite, token string) int {
		form := url.Values{}
		if token != "" {
			form.Set(csrfField, token)
		}
		r := httptest.NewRequest(http.MethodPost, "http://"+testHost+"/check", strings.NewReader(form.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		if fetchSite != "" {
			r.Header.Set("Sec-Fetch-Site", fetchSite)
		}
		r.AddCookie(c.cookie)
		return do(s, r).Code
	}
	cases := []struct {
		name, origin, site, token string
		want                      int
	}{
		{"same origin with token", "http://" + testHost, "", c.csrf, http.StatusSeeOther},
		{"no origin, same-origin fetch", "", "same-origin", c.csrf, http.StatusSeeOther},
		{"cross origin", "http://evil.example", "", c.csrf, http.StatusForbidden},
		{"other allowed host", "http://localhost:7878", "", c.csrf, http.StatusForbidden},
		{"null origin", "null", "", c.csrf, http.StatusForbidden},
		{"no origin, cross-site fetch", "", "cross-site", c.csrf, http.StatusForbidden},
		{"no origin at all", "", "", c.csrf, http.StatusForbidden},
		{"missing token", "http://" + testHost, "", "", http.StatusForbidden},
		{"wrong token", "http://" + testHost, "", "x" + c.csrf, http.StatusForbidden},
	}
	for _, tc := range cases {
		if got := send(tc.origin, tc.site, tc.token); got != tc.want {
			t.Errorf("%s: %d, want %d", tc.name, got, tc.want)
		}
	}

	hdr := httptest.NewRequest(http.MethodPost, "http://"+testHost+"/check", nil)
	hdr.Header.Set("Origin", "http://"+testHost)
	hdr.Header.Set(csrfHeader, c.csrf)
	hdr.AddCookie(c.cookie)
	if rec := do(s, hdr); rec.Code != http.StatusSeeOther {
		t.Errorf("CSRF in the header: %d", rec.Code)
	}
}

func TestReadOnlyHasNoWriteRoutes(t *testing.T) {
	s, _ := newTestServer(t, func(o *Options) { o.ReadOnly = true })
	c := signIn(t, s)
	for _, path := range []string{"/packs/icon-sets-flag/sync", "/sync-all", "/config/packs.json"} {
		if rec := c.post(path, nil); rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("POST %s: %d, want 405", path, rec.Code)
		}
	}
	// Refreshing the check writes nothing and stays available.
	if rec := c.post("/check", nil); rec.Code != http.StatusSeeOther {
		t.Errorf("POST /check read-only: %d", rec.Code)
	}
}

func TestSessionsAreBounded(t *testing.T) {
	var ss sessions
	first, _ := ss.create(fixedNow)
	for i := 0; i < maxSessions; i++ {
		ss.create(fixedNow.Add(1))
	}
	if len(ss.byID) != maxSessions {
		t.Fatalf("%d sessions", len(ss.byID))
	}
	if ss.get(first) != nil {
		t.Error("the oldest session survived eviction")
	}
}

func TestNewTokenIsLongAndUnique(t *testing.T) {
	a, b := NewToken(), NewToken()
	if a == b || len(a) < 43 {
		t.Fatalf("tokens %q %q", a, b)
	}
}

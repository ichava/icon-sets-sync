package webhooks

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ichava/icon-sets-sync/src/internal/core/settings"
	"github.com/ichava/icon-sets-sync/src/internal/events"
)

// Guard: a bad signature does not verify. Proven red by making Verify
// return nil after the timestamp check: the tampered cases passed.
func TestSignatureVerifiesAndRejectsTampering(t *testing.T) {
	secret, other := NewSecret(), NewSecret()
	now := time.Unix(1_800_000_000, 0)
	body := []byte(`{"type":"job.finished"}`)
	h := http.Header{}
	h.Set(HeaderID, "01JCX0000000000000000000AB")
	h.Set(HeaderTimestamp, "1800000000")
	h.Set(HeaderSignature, SignatureHeader([]string{secret}, "01JCX0000000000000000000AB", now, body))
	if err := Verify(secret, h, body, now, 0); err != nil {
		t.Fatalf("a good signature: %v", err)
	}
	for name, c := range map[string]struct {
		secret string
		body   []byte
		now    time.Time
		mutate func(http.Header)
		want   error
	}{
		"another secret":   {other, body, now, nil, ErrSignature},
		"a changed body":   {secret, []byte(`{"type":"job.started"}`), now, nil, ErrSignature},
		"a changed id":     {secret, body, now, func(h http.Header) { h.Set(HeaderID, "01JCX0000000000000000000AC") }, ErrSignature},
		"a replayed stamp": {secret, body, now.Add(10 * time.Minute), nil, ErrTimestamp},
		"no signature":     {secret, body, now, func(h http.Header) { h.Del(HeaderSignature) }, ErrNoSignature},
		"a forged v1": {secret, body, now, func(h http.Header) {
			h.Set(HeaderSignature, "v1,"+strings.Repeat("A", 43)+"=")
		}, ErrSignature},
	} {
		hh := h.Clone()
		if c.mutate != nil {
			c.mutate(hh)
		}
		if err := Verify(c.secret, hh, c.body, c.now, 0); !errors.Is(err, c.want) {
			t.Errorf("%s: %v, want %v", name, err, c.want)
		}
	}
}

// The Standard Webhooks reference vector: the key is the base64 after
// whsec_, the signed content id.timestamp.body.
func TestSignMatchesTheStandardWebhooksVector(t *testing.T) {
	// From the Standard Webhooks specification's examples.
	// @credential-literal-exempt: the specification's published test vector
	secret := "whsec_MfKQ9r8GKYqrTwjUPD8ILPZIo2LaLaSw"
	got := Sign(secret, "msg_p5jXN8AQM9LWM0D4loKWxJek", time.Unix(1614265330, 0), []byte(`{"test": 2432232314}`))
	if got != "v1,g0hM9SsE+OTPJTGt/tmIKtSyZlE3uFJELVlNIOLJ1OE=" {
		t.Fatalf("got %s", got)
	}
}

// Guard: a webhook to a private address is refused at connect time. Proven
// red by making control return nil: the dial reached the loopback address.
func TestPrivateAddressesAreRefusedAtConnect(t *testing.T) {
	p := Policy{}
	for _, addr := range []string{
		"127.0.0.1:443", "10.1.2.3:443", "172.16.0.1:443", "192.168.1.1:443", "169.254.169.254:80",
		"100.64.0.1:443", "0.0.0.0:443", "[::1]:443", "[fd00:ec2::254]:80", "[fe80::1]:443", "[::ffff:127.0.0.1]:443",
		"[64:ff9b::a9fe:a9fe]:80", "[2002:7f00:1::]:443",
	} {
		if err := p.control(false)("tcp", addr, nil); !errors.Is(err, ErrDenied) {
			t.Errorf("%s: %v, want refused", addr, err)
		}
	}
	for _, addr := range []string{"93.184.215.14:443", "[2606:2800:21f:cb07:6820:80da:af6b:8b2c]:443", "1.1.1.1:443"} {
		if err := p.control(false)("tcp", addr, nil); err != nil {
			t.Errorf("%s: %v, want allowed", addr, err)
		}
	}
	if err := (Policy{AllowPrivate: true}).control(false)("tcp", "10.1.2.3:443", nil); err != nil {
		t.Errorf("allow_private: %v", err)
	}
	if err := p.control(true)("tcp", "127.0.0.1:8080", nil); err != nil {
		t.Errorf("the localhost exception: %v", err)
	}
	if err := p.control(true)("tcp", "10.0.0.1:8080", nil); !errors.Is(err, ErrDenied) {
		t.Errorf("the localhost exception opened the LAN: %v", err)
	}
	if !Denied(netip.MustParseAddr("::ffff:10.0.0.1")) {
		t.Error("an IPv4-mapped private address passed")
	}
}

// A name that resolves to loopback (as a rebinding attack would arrange) is
// refused when it is dialled, through the real Send path.
func TestSendRefusesANameResolvingToLoopback(t *testing.T) {
	s := &Sender{Secrets: secretsFor(t, "ci")}
	for _, u := range []string{"https://localhost:9/hook", "https://127.0.0.1:9/hook", "https://[::1]:9/hook"} {
		r := s.Send(context.Background(), settings.Webhook{Name: "ci", URL: u, Events: []string{events.JobFinished}}, testEvent(t))
		if !errors.Is(r.Err, ErrDenied) || !r.Permanent {
			t.Errorf("%s: %v (permanent %t), want refused", u, r.Err, r.Permanent)
		}
	}
}

func TestURLRules(t *testing.T) {
	strict, local := Policy{}, Policy{AllowInsecureLocalhost: true}
	for _, c := range []struct {
		p    Policy
		url  string
		ok   bool
		loop bool
	}{
		{strict, "https://hooks.example.com/x", true, false},
		{strict, "http://hooks.example.com/x", false, false},
		{strict, "http://localhost:8080/x", false, false},
		{local, "http://localhost:8080/x", true, true},
		{local, "http://127.0.0.1:8080/x", true, true},
		{local, "http://hooks.example.com/x", false, false},
		{strict, "ftp://hooks.example.com/x", false, false},
		{strict, "https://user:pw@hooks.example.com/x", false, false},
	} {
		loop, err := c.p.CheckURL(c.url)
		if (err == nil) != c.ok || loop != c.loop {
			t.Errorf("%s (%+v): loopback %t, err %v", c.url, c.p, loop, err)
		}
	}
}

func secretsFor(t *testing.T, names ...string) *Secrets {
	t.Helper()
	s := &Secrets{Path: filepath.Join(t.TempDir(), SecretsFile)}
	for _, n := range names {
		if _, err := s.Create(n); err != nil {
			t.Fatal(err)
		}
	}
	return s
}

func testEvent(t *testing.T) events.Event {
	t.Helper()
	e, err := events.New("icon-sets-sync://test", events.JobFinished, time.Now(), map[string]any{
		"job_id": "20261112T100000Z-abcdef", "kind": "sync", "packs": []string{"icon-sets-flag"}, "dry_run": true,
		"status": "succeeded", "duration_ms": 5,
	})
	if err != nil {
		t.Fatal(err)
	}
	return e
}

// receiver is an in-memory receiver: the sandbox cannot bind a port, so the
// sender's transport hands the request straight to a handler.
type receiver struct {
	mu     sync.Mutex
	status int
	got    []*http.Request
	bodies [][]byte
}

func (r *receiver) RoundTrip(req *http.Request) (*http.Response, error) {
	body, _ := io.ReadAll(req.Body)
	r.mu.Lock()
	r.got = append(r.got, req)
	r.bodies = append(r.bodies, body)
	status := r.status
	r.mu.Unlock()
	rec := httptest.NewRecorder()
	rec.WriteHeader(status)
	return rec.Result(), nil
}

func (r *receiver) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.got)
}

func TestDeliveryIsSignedAndRotationSignsTwice(t *testing.T) {
	secrets := secretsFor(t, "ci")
	recv := &receiver{status: http.StatusNoContent}
	clock := time.Now()
	secrets.Now = func() time.Time { return clock }
	s := &Sender{Secrets: secrets, transport: recv, Now: func() time.Time { return clock }}
	w := settings.Webhook{Name: "ci", URL: "https://ci.example.com/hook", Events: []string{events.JobFinished}}
	old, _ := secrets.Signing(w)
	if r := s.Send(context.Background(), w, testEvent(t)); r.Err != nil {
		t.Fatal(r.Err)
	}
	if err := Verify(old[0], recv.got[0].Header, recv.bodies[0], clock, 0); err != nil {
		t.Fatalf("receiver check: %v", err)
	}
	var env events.Event
	if err := json.Unmarshal(recv.bodies[0], &env); err != nil || env.SpecVersion != "1.0" || recv.got[0].Header.Get(HeaderID) != env.ID {
		t.Fatalf("body %s, id header %s", recv.bodies[0], recv.got[0].Header.Get(HeaderID))
	}

	fresh, err := secrets.Rotate("ci")
	if err != nil {
		t.Fatal(err)
	}
	if r := s.Send(context.Background(), w, testEvent(t)); r.Err != nil {
		t.Fatal(r.Err)
	}
	h := recv.got[1].Header
	if n := len(strings.Fields(h.Get(HeaderSignature))); n != 2 {
		t.Fatalf("%d signatures during the grace period, want 2", n)
	}
	for _, sec := range []string{old[0], fresh} {
		if err := Verify(sec, h, recv.bodies[1], clock, 0); err != nil {
			t.Errorf("a receiver on either secret verifies: %v", err)
		}
	}
	clock = clock.Add(RotationGrace + time.Minute)
	if r := s.Send(context.Background(), w, testEvent(t)); r.Err != nil {
		t.Fatal(r.Err)
	}
	if err := Verify(old[0], recv.got[2].Header, recv.bodies[2], clock, 0); !errors.Is(err, ErrSignature) {
		t.Fatalf("the old secret still signs after the grace: %v", err)
	}
}

func TestSecretEnvOverridesTheGeneratedSecret(t *testing.T) {
	secrets := secretsFor(t, "ci")
	secrets.Lookup = func(k string) (string, bool) {
		if k == "CI_HOOK_SECRET" {
			return "from-the-environment", true
		}
		return "", false
	}
	got, err := secrets.Signing(settings.Webhook{Name: "ci", SecretEnv: "CI_HOOK_SECRET"})
	if err != nil || len(got) != 1 || got[0] != "from-the-environment" {
		t.Fatalf("%v, %v", got, err)
	}
	if info, err := os.Stat(secrets.Path); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("secrets file: %v", err)
	}
}

func TestOversizedPayloadIsNotSent(t *testing.T) {
	recv := &receiver{status: http.StatusOK}
	s := &Sender{Secrets: secretsFor(t, "ci"), transport: recv}
	e := testEvent(t)
	e.Data = json.RawMessage(`"` + strings.Repeat("a", MaxPayload) + `"`)
	r := s.Send(context.Background(), settings.Webhook{Name: "ci", URL: "https://ci.example.com/hook"}, e)
	if !errors.Is(r.Err, ErrTooLarge) || recv.count() != 0 {
		t.Fatalf("%v, %d sent", r.Err, recv.count())
	}
}

func TestRedirectIsAFailure(t *testing.T) {
	recv := &receiver{status: http.StatusFound}
	s := &Sender{Secrets: secretsFor(t, "ci"), transport: recv}
	r := s.Send(context.Background(), settings.Webhook{Name: "ci", URL: "https://ci.example.com/hook"}, testEvent(t))
	if r.Err == nil || r.Status != http.StatusFound || recv.count() != 1 {
		t.Fatalf("a 302 must fail and not be followed: %v, %d, %d requests", r.Err, r.Status, recv.count())
	}
}

func TestDispatcherRetriesThenDisables(t *testing.T) {
	dir := t.TempDir()
	recv := &receiver{status: http.StatusInternalServerError}
	bus := events.NewBus("icon-sets-sync://test", nil)
	w := settings.Webhook{Name: "ci", URL: "https://ci.example.com/hook", Events: []string{events.JobFinished}}
	d := NewDispatcher(dir, []settings.Webhook{w}, &Sender{Secrets: secretsFor(t, "ci"), transport: recv}, bus)
	d.Backoff = []time.Duration{time.Millisecond, time.Millisecond}
	watch := bus.Subscribe(10, events.WebhookDisabled)
	sub := bus.Subscribe(100)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { d.Run(ctx, sub); close(done) }()

	bus.Publish(testEvent(t))
	deadline := time.Now().Add(5 * time.Second)
	for recv.count() < 3 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if recv.count() != 3 {
		t.Fatalf("%d attempts, want the first and two retries", recv.count())
	}
	time.Sleep(20 * time.Millisecond)
	if recv.count() != 3 {
		t.Fatalf("%d attempts after the backoff ran out", recv.count())
	}
	recs, _ := d.Log.Tail(0)
	outcomes := []string{}
	for _, raw := range recs {
		var r Record
		_ = json.Unmarshal(raw, &r)
		outcomes = append(outcomes, r.Outcome)
	}
	if strings.Join(outcomes, ",") != "retrying,retrying,failed" {
		t.Fatalf("delivery log %v", outcomes)
	}

	d.setState("ci", func(s *endpointState) { s.ConsecutiveFailures = DisableAfter - 1 })
	bus.Publish(testEvent(t))
	select {
	case e := <-watch.C():
		if err := events.Validate(e); err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no webhook.disabled after the 50th consecutive failure")
	}
	if !Disabled(dir, "ci") {
		t.Fatal("not disabled")
	}
	before := recv.count()
	bus.Publish(testEvent(t))
	time.Sleep(20 * time.Millisecond)
	if recv.count() != before {
		t.Fatal("a disabled endpoint still received")
	}

	recv.mu.Lock()
	recv.status = http.StatusOK
	recv.mu.Unlock()
	if _, r := d.Test(ctx, w); r.Err != nil || Disabled(dir, "ci") {
		t.Fatalf("a successful test turns it back on: %v", r.Err)
	}
	cancel()
	<-done
}

func TestStreamOnlyTypesNeverReachAWebhook(t *testing.T) {
	w := settings.Webhook{Events: []string{events.JobProgress, events.JobFinished}}
	if wants(w, events.JobProgress) || !wants(w, events.JobFinished) || wants(w, events.JobStarted) {
		t.Fatal("event filter")
	}
}

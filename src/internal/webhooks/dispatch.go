package webhooks

import (
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"sync"
	"time"

	"github.com/ichava/icon-sets-sync/src/internal/core/jsonlog"
	"github.com/ichava/icon-sets-sync/src/internal/core/settings"
	"github.com/ichava/icon-sets-sync/src/internal/core/statefile"
	"github.com/ichava/icon-sets-sync/src/internal/events"
)

// Files in the state directory.
const (
	DeliveriesFile = "webhook-deliveries.jsonl"
	StateFile      = "webhook-state.json"
)

// QueueSize bounds the deliveries waiting; one more is dropped and logged.
const QueueSize = 1000

// DisableAfter is how many consecutive failed attempts turn an endpoint off.
const DisableAfter = 50

// DefaultBackoff is the wait before each retry: five retries after the
// first attempt, then the delivery is marked failed.
var DefaultBackoff = []time.Duration{time.Minute, 5 * time.Minute, 30 * time.Minute, 2 * time.Hour, 6 * time.Hour}

// Record is one line of the delivery log.
type Record struct {
	Time     time.Time `json:"time"`
	Endpoint string    `json:"endpoint"`
	EventID  string    `json:"event_id"`
	Type     string    `json:"type"`
	Attempt  int       `json:"attempt"`
	// Outcome is delivered, retrying, failed, dropped or disabled.
	Outcome    string    `json:"outcome"`
	Status     int       `json:"status,omitempty"`
	Error      string    `json:"error,omitempty"`
	DurationMs int64     `json:"duration_ms"`
	NextTry    time.Time `json:"next_try,omitzero"`
}

// endpointState is what the dispatcher remembers about an endpoint across
// restarts.
type endpointState struct {
	ConsecutiveFailures int       `json:"consecutive_failures"`
	Disabled            bool      `json:"disabled"`
	DisabledAt          time.Time `json:"disabled_at,omitzero"`
}

type delivery struct {
	w       settings.Webhook
	e       events.Event
	attempt int
}

// Dispatcher delivers the bus's events to the configured endpoints.
type Dispatcher struct {
	Endpoints []settings.Webhook
	Sender    *Sender
	// Log is the delivery log; StatePath the endpoint state file.
	Log       *jsonlog.Log
	StatePath string
	// Bus receives webhook.disabled.
	Bus *events.Bus
	// Backoff is the wait before each retry; nil is DefaultBackoff.
	Backoff []time.Duration
	// Now is the clock; nil means time.Now.
	Now func() time.Time

	queue chan delivery
	mu    sync.Mutex
	wg    sync.WaitGroup
}

// NewDispatcher wires a dispatcher to the state directory.
func NewDispatcher(stateDir string, endpoints []settings.Webhook, sender *Sender, bus *events.Bus) *Dispatcher {
	return &Dispatcher{
		Endpoints: endpoints, Sender: sender, Bus: bus,
		Log:       &jsonlog.Log{Path: filepath.Join(stateDir, DeliveriesFile), Keep: 3},
		StatePath: filepath.Join(stateDir, StateFile),
	}
}

func (d *Dispatcher) now() time.Time {
	if d.Now != nil {
		return d.Now()
	}
	return time.Now()
}

func (d *Dispatcher) backoff() []time.Duration {
	if d.Backoff != nil {
		return d.Backoff
	}
	return DefaultBackoff
}

// Run delivers what sub receives until ctx ends.
func (d *Dispatcher) Run(ctx context.Context, sub *events.Subscription) {
	d.queue = make(chan delivery, QueueSize)
	d.wg.Add(1)
	go func() {
		defer d.wg.Done()
		d.work(ctx)
	}()
	for {
		select {
		case <-ctx.Done():
			sub.Close()
			d.wg.Wait()
			return
		case e, ok := <-sub.C():
			if !ok {
				d.wg.Wait()
				return
			}
			for _, w := range d.Endpoints {
				if wants(w, e.Type) && !d.disabled(w.Name) {
					d.enqueue(delivery{w: w, e: e, attempt: 1})
				}
			}
		}
	}
}

func wants(w settings.Webhook, typ string) bool {
	if events.StreamOnly(typ) {
		return false
	}
	for _, t := range w.Events {
		if t == typ {
			return true
		}
	}
	return false
}

func (d *Dispatcher) enqueue(job delivery) {
	select {
	case d.queue <- job:
	default:
		d.record(job, Result{Err: errors.New("queue full")}, "dropped", time.Time{})
	}
}

func (d *Dispatcher) work(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case job := <-d.queue:
			if d.disabled(job.w.Name) {
				continue
			}
			d.attempt(ctx, job)
		}
	}
}

// attempt makes one delivery and schedules the next on failure.
func (d *Dispatcher) attempt(ctx context.Context, job delivery) {
	r := d.Sender.Send(ctx, job.w, job.e)
	if r.Err == nil {
		d.setState(job.w.Name, func(s *endpointState) { s.ConsecutiveFailures = 0 })
		d.record(job, r, "delivered", time.Time{})
		return
	}
	var failures int
	d.setState(job.w.Name, func(s *endpointState) {
		s.ConsecutiveFailures++
		failures = s.ConsecutiveFailures
		if failures >= DisableAfter && !s.Disabled {
			s.Disabled, s.DisabledAt = true, d.now().UTC()
		}
	})
	retries := d.backoff()
	if r.Permanent || job.attempt > len(retries) || ctx.Err() != nil {
		d.record(job, r, "failed", time.Time{})
	} else {
		wait := retries[job.attempt-1]
		d.record(job, r, "retrying", d.now().Add(wait))
		next := delivery{w: job.w, e: job.e, attempt: job.attempt + 1}
		time.AfterFunc(wait, func() {
			if ctx.Err() == nil {
				d.enqueue(next)
			}
		})
	}
	if failures == DisableAfter {
		d.record(job, r, "disabled", time.Time{})
		d.Bus.Emit(events.WebhookDisabled, map[string]any{"name": job.w.Name, "consecutive_failures": failures})
		slog.Warn("webhook disabled after consecutive failures", "endpoint", job.w.Name, "failures", failures)
	}
}

// Test sends a webhook.test event to w now, whatever its event filter and
// even when it is disabled, and turns it back on when the delivery succeeds.
func (d *Dispatcher) Test(ctx context.Context, w settings.Webhook) (events.Event, Result) {
	e, err := events.New(events.Source(), events.WebhookTest, d.now(),
		map[string]any{"name": w.Name, "message": "A test delivery from icon-sets-sync."})
	if err != nil {
		return e, Result{Err: err, Permanent: true}
	}
	job := delivery{w: w, e: e, attempt: 1}
	r := d.Sender.Send(ctx, w, e)
	if r.Err == nil {
		d.setState(w.Name, func(s *endpointState) { *s = endpointState{} })
		d.record(job, r, "delivered", time.Time{})
	} else {
		d.record(job, r, "failed", time.Time{})
	}
	return e, r
}

func (d *Dispatcher) record(job delivery, r Result, outcome string, next time.Time) {
	rec := Record{
		Time: d.now().UTC(), Endpoint: job.w.Name, EventID: job.e.ID, Type: job.e.Type, Attempt: job.attempt,
		Outcome: outcome, Status: r.Status, DurationMs: r.Duration.Milliseconds(), NextTry: next,
	}
	if r.Err != nil {
		rec.Error = r.Err.Error()
	}
	if err := d.Log.Append(rec); err != nil {
		slog.Warn("webhook delivery log not written", "err", err)
	}
}

// States reads every endpoint's state.
func (d *Dispatcher) States() map[string]endpointState {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.loadStates()
}

func (d *Dispatcher) loadStates() map[string]endpointState {
	all := map[string]endpointState{}
	if d.StatePath != "" {
		if _, err := statefile.Load(d.StatePath, &all); err != nil {
			slog.Warn("webhook state not read", "err", err)
		}
	}
	return all
}

func (d *Dispatcher) disabled(name string) bool { return d.States()[name].Disabled }

func (d *Dispatcher) setState(name string, change func(*endpointState)) {
	d.mu.Lock()
	defer d.mu.Unlock()
	all := d.loadStates()
	st := all[name]
	change(&st)
	all[name] = st
	if d.StatePath == "" {
		return
	}
	if err := statefile.Save(d.StatePath, all); err != nil {
		slog.Warn("webhook state not saved", "err", err)
	}
}

// Disabled reports whether name was turned off after DisableAfter failures.
func Disabled(stateDir, name string) bool {
	all := map[string]endpointState{}
	_, _ = statefile.Load(filepath.Join(stateDir, StateFile), &all)
	return all[name].Disabled
}

// Package jobs runs the web UI's long operations -- syncs, recipes, audits --
// in the background, records what each one did as a stream of typed events,
// and keeps a bounded history on disk. It knows nothing about HTTP.
package jobs

import (
	"sync"
	"time"
)

// Event types.
const (
	EventLog       = "log"
	EventStage     = "stage"
	EventPackStart = "pack-start"
	EventPackDone  = "pack-done"
	EventDone      = "done"
)

// Stage states, on EventStage.
const (
	StageStarted  = "started"
	StageFinished = "finished"
	StageFailed   = "failed"
)

// Event is one thing that happened in a job. ID increases by one per event
// within a job, so a reconnecting EventSource resumes exactly where it left
// off with Last-Event-ID.
type Event struct {
	ID   int64     `json:"id"`
	Type string    `json:"type"`
	Time time.Time `json:"time"`

	// log
	Level string            `json:"level,omitempty"`
	Msg   string            `json:"msg,omitempty"`
	Attrs map[string]string `json:"attrs,omitempty"`

	// stage
	Pipeline string         `json:"pipeline,omitempty"`
	Stage    string         `json:"stage,omitempty"`
	State    string         `json:"state,omitempty"`
	Metrics  map[string]any `json:"metrics,omitempty"`

	// pack-start, pack-done, done
	Pack   string `json:"pack,omitempty"`
	Status string `json:"status,omitempty"`
	Err    string `json:"error,omitempty"`
}

// DefaultCapacity is how many events a job keeps in memory: enough for the
// bundled pack's 72 sets, a ceiling against a runaway logger.
const DefaultCapacity = 10000

// ring is a bounded, append-only event log with change notification.
type ring struct {
	mu      sync.Mutex
	cap     int
	events  []Event
	nextID  int64
	dropped int64
	closed  bool
	notify  chan struct{}
}

func newRing(capacity int) *ring {
	if capacity < 1 {
		capacity = DefaultCapacity
	}
	return &ring{cap: capacity, nextID: 1, notify: make(chan struct{})}
}

// append assigns the next ID, stores the event, and wakes every waiter.
// After close it is a no-op: a late log line from a cancelled stage must not
// land after the done event.
func (r *ring) append(e Event, now time.Time) (Event, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return Event{}, false
	}
	e.ID = r.nextID
	r.nextID++
	if e.Time.IsZero() {
		e.Time = now
	}
	if len(r.events) == r.cap {
		copy(r.events, r.events[1:])
		r.events = r.events[:r.cap-1]
		r.dropped++
	}
	r.events = append(r.events, e)
	close(r.notify)
	r.notify = make(chan struct{})
	return e, true
}

func (r *ring) close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.closed {
		r.closed = true
		close(r.notify)
		r.notify = make(chan struct{})
	}
}

// since returns the retained events after id, whether the ring is closed,
// and a channel closed on the next change.
func (r *ring) since(id int64) ([]Event, bool, <-chan struct{}) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []Event
	for _, e := range r.events {
		if e.ID > id {
			out = append(out, e)
		}
	}
	return out, r.closed, r.notify
}

func (r *ring) all() []Event {
	evs, _, _ := r.since(0)
	return evs
}

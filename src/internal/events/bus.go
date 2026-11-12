package events

import (
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

// DefaultBuffer is a subscriber's queue length. A subscriber that falls this
// far behind loses events, counted, rather than slowing a sync down.
const DefaultBuffer = 1000

// Bus is a typed, in-process publish/subscribe bus. Publishing never blocks:
// a full subscriber drops the event and counts it.
type Bus struct {
	source string
	now    func() time.Time

	mu   sync.Mutex
	subs map[*Subscription]struct{}
}

// NewBus returns a bus whose events carry source (see Source). now is the
// clock; nil means time.Now.
func NewBus(source string, now func() time.Time) *Bus {
	if now == nil {
		now = time.Now
	}
	return &Bus{source: source, now: now, subs: map[*Subscription]struct{}{}}
}

// Emit builds an event of typ with data and publishes it. An event that
// cannot be built (an unknown type, unencodable data) is a programming error:
// it is logged and dropped rather than taking a sync down with it.
func (b *Bus) Emit(typ string, data any) {
	if b == nil {
		return
	}
	e, err := New(b.source, typ, b.now(), data)
	if err != nil {
		slog.Error("event not published", "type", typ, "err", err)
		return
	}
	b.Publish(e)
}

// Publish hands e to every subscriber whose filter admits it.
func (b *Bus) Publish(e Event) {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	for s := range b.subs {
		if !s.admits(e.Type) {
			continue
		}
		select {
		case s.ch <- e:
		default:
			s.dropped.Add(1)
		}
	}
}

// Subscribe returns a subscription to the given types (none means every
// type), with a queue of buffer events (DefaultBuffer when below 1).
func (b *Bus) Subscribe(buffer int, types ...string) *Subscription {
	if buffer < 1 {
		buffer = DefaultBuffer
	}
	s := &Subscription{bus: b, ch: make(chan Event, buffer)}
	if len(types) > 0 {
		s.only = map[string]bool{}
		for _, t := range types {
			s.only[t] = true
		}
	}
	b.mu.Lock()
	b.subs[s] = struct{}{}
	b.mu.Unlock()
	return s
}

// Subscription is one consumer's queue.
type Subscription struct {
	bus     *Bus
	ch      chan Event
	only    map[string]bool
	dropped atomic.Int64
	once    sync.Once
}

func (s *Subscription) admits(t string) bool { return s.only == nil || s.only[t] }

// C delivers the events. It is closed by Close.
func (s *Subscription) C() <-chan Event { return s.ch }

// Dropped counts the events lost because the queue was full.
func (s *Subscription) Dropped() int64 { return s.dropped.Load() }

// Close stops delivery and closes C.
func (s *Subscription) Close() {
	s.once.Do(func() {
		s.bus.mu.Lock()
		delete(s.bus.subs, s)
		s.bus.mu.Unlock()
		close(s.ch)
	})
}

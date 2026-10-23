package jobs

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"
)

// Kind is what a job does.
type Kind string

// The kinds of job the web UI runs.
const (
	KindSync    Kind = "sync"
	KindSyncAll Kind = "sync-all"
	KindRecipe  Kind = "recipe"
	KindAudit   Kind = "audit"
)

// Status is how a job stands or ended.
type Status string

// Job statuses.
const (
	StatusRunning   Status = "running"
	StatusSucceeded Status = "succeeded"
	StatusFailed    Status = "failed"
	StatusCancelled Status = "cancelled"
)

// Spec describes a job to start.
type Spec struct {
	Kind   Kind
	Packs  []string // the packs it touches, locked for its duration
	DryRun bool
	// Label is a one-line description for the history, e.g. "recipe 3.49.0".
	Label string
}

// Info is a job's metadata, as listed and persisted.
type Info struct {
	ID      string    `json:"id"`
	Kind    Kind      `json:"kind"`
	Packs   []string  `json:"packs"`
	DryRun  bool      `json:"dry_run"`
	Label   string    `json:"label,omitempty"`
	Started time.Time `json:"started"`
	Ended   time.Time `json:"ended,omitzero"`
	Status  Status    `json:"status"`
	Err     string    `json:"error,omitempty"`
}

// Duration is how long the job ran, or has been running.
func (i Info) Duration(now time.Time) time.Duration {
	end := i.Ended
	if end.IsZero() {
		end = now
	}
	return end.Sub(i.Started).Round(time.Second)
}

// Job is one background operation.
type Job struct {
	mu     sync.Mutex
	info   Info
	ring   *ring
	now    func() time.Time
	cancel context.CancelFunc
	done   chan struct{}
	// Result carries a kind-specific payload (the audit's resolutions) for
	// the page that renders the finished job. Set by the run function.
	result any
}

// Info returns a snapshot of the job's metadata.
func (j *Job) Info() Info {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.info
}

// Events returns every retained event.
func (j *Job) Events() []Event { return j.ring.all() }

// Since returns the events after id, whether the job has finished, and a
// channel closed on the next event or on finish.
func (j *Job) Since(id int64) ([]Event, bool, <-chan struct{}) { return j.ring.since(id) }

// Done is closed when the job has finished and its done event is recorded.
func (j *Job) Done() <-chan struct{} { return j.done }

// SetResult stores the kind-specific payload.
func (j *Job) SetResult(v any) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.result = v
}

// Result returns the payload SetResult stored.
func (j *Job) Result() any {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.result
}

// Logf records a log line from the job's own orchestration.
func (j *Job) Logf(level, format string, a ...any) {
	j.append(Event{Type: EventLog, Level: level, Msg: fmt.Sprintf(format, a...)})
}

// PackStarted and PackDone record a pack's boundaries.
func (j *Job) PackStarted(pack, detail string) {
	j.append(Event{Type: EventPackStart, Pack: pack, Msg: detail})
}

// PackDone records how one pack ended.
func (j *Job) PackDone(pack, status, errText, detail string) {
	j.append(Event{Type: EventPackDone, Pack: pack, Status: status, Err: errText, Msg: detail})
}

func (j *Job) append(e Event) { j.ring.append(e, j.now()) }

// ErrBusy is returned when a pack the job needs is already being worked on.
type ErrBusy struct {
	Pack  string
	JobID string
}

func (e *ErrBusy) Error() string {
	return fmt.Sprintf("%s is already being synced by job %s", e.Pack, e.JobID)
}

// RunFunc does the work. It must honour ctx; the status it returns is the
// job's outcome unless ctx was cancelled, which always reads as cancelled.
type RunFunc func(ctx context.Context, j *Job) (Status, error)

// Registry starts jobs, enforces one job per pack, and keeps them findable.
type Registry struct {
	mu    sync.Mutex
	jobs  map[string]*Job
	order []string
	locks map[string]string // pack -> job ID
	store *Store
	now   func() time.Time
	base  context.Context
	cap   int
	wg    sync.WaitGroup
}

// NewRegistry returns a Registry. store may be nil (no history on disk); base
// is cancelled to stop every job, as on shutdown.
func NewRegistry(base context.Context, store *Store, now func() time.Time) *Registry {
	if now == nil {
		now = time.Now
	}
	return &Registry{jobs: map[string]*Job{}, locks: map[string]string{}, store: store, now: now, base: base, cap: DefaultCapacity}
}

// SetCapacity sets the per-job event capacity; for tests.
func (r *Registry) SetCapacity(n int) { r.cap = n }

// Start runs fn in the background as a new job, after taking every pack's
// lock. A pack already locked fails the whole start with *ErrBusy, and no
// lock is taken: two runs racing on one pack repository's `git checkout -B`
// is exactly what the lock exists to prevent.
func (r *Registry) Start(spec Spec, fn RunFunc) (*Job, error) {
	r.mu.Lock()
	for _, p := range spec.Packs {
		if id, busy := r.locks[p]; busy {
			r.mu.Unlock()
			return nil, &ErrBusy{Pack: p, JobID: id}
		}
	}
	ctx, cancel := context.WithCancel(r.base)
	j := &Job{
		info: Info{
			ID: newID(r.now()), Kind: spec.Kind, Packs: append([]string(nil), spec.Packs...),
			DryRun: spec.DryRun, Label: spec.Label, Started: r.now(), Status: StatusRunning,
		},
		ring: newRing(r.cap), now: r.now, cancel: cancel, done: make(chan struct{}),
	}
	for _, p := range spec.Packs {
		r.locks[p] = j.info.ID
	}
	r.jobs[j.info.ID] = j
	r.order = append(r.order, j.info.ID)
	r.wg.Add(1)
	r.mu.Unlock()

	go r.run(ctx, j, fn)
	return j, nil
}

func (r *Registry) run(ctx context.Context, j *Job, fn RunFunc) {
	defer r.wg.Done()
	status, err := safeRun(ctx, j, fn)
	if ctx.Err() != nil && status != StatusSucceeded {
		status = StatusCancelled
	}
	j.mu.Lock()
	j.info.Status, j.info.Ended = status, r.now()
	if err != nil {
		j.info.Err = err.Error()
	}
	info := j.info
	j.mu.Unlock()

	j.append(Event{Type: EventDone, Status: string(status), Err: info.Err})
	j.ring.close()
	j.cancel()

	r.mu.Lock()
	for _, p := range info.Packs {
		if r.locks[p] == info.ID {
			delete(r.locks, p)
		}
	}
	r.mu.Unlock()

	if r.store != nil {
		if err := r.store.Save(info, j.Events()); err != nil {
			j.mu.Lock()
			j.info.Err = joinErr(j.info.Err, "history not saved: "+err.Error())
			j.mu.Unlock()
		}
	}
	close(j.done)
}

// safeRun turns a panic in a run into a failed job, not a dead server.
func safeRun(ctx context.Context, j *Job, fn RunFunc) (status Status, err error) {
	defer func() {
		if p := recover(); p != nil {
			status, err = StatusFailed, fmt.Errorf("internal error: %v", p)
		}
	}()
	return fn(ctx, j)
}

func joinErr(a, b string) string {
	if a == "" {
		return b
	}
	return a + "; " + b
}

// Get returns a job started by this process.
func (r *Registry) Get(id string) *Job {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.jobs[id]
}

// Cancel asks a running job to stop. It reports whether the job exists.
func (r *Registry) Cancel(id string) bool {
	j := r.Get(id)
	if j == nil {
		return false
	}
	j.cancel()
	return true
}

// Running returns the job holding a pack's lock, if any.
func (r *Registry) Running(pack string) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.locks[pack]
}

// List returns this process's jobs and the persisted history, newest first,
// each ID once.
func (r *Registry) List() []Info {
	r.mu.Lock()
	seen := map[string]bool{}
	var out []Info
	for _, id := range r.order {
		out = append(out, r.jobs[id].Info())
		seen[id] = true
	}
	r.mu.Unlock()
	if r.store != nil {
		stored, _ := r.store.List()
		for _, i := range stored {
			if !seen[i.ID] {
				out = append(out, i)
			}
		}
	}
	sort.SliceStable(out, func(a, b int) bool { return out[a].Started.After(out[b].Started) })
	return out
}

// Wait blocks until every job has finished; for shutdown and tests.
func (r *Registry) Wait() { r.wg.Wait() }

// ErrNotFound is returned for a job ID this process and the history lack.
var ErrNotFound = errors.New("no such job")

// newID is sortable by start time, so the history's file names list in order.
func newID(now time.Time) string {
	b := make([]byte, 3)
	_, _ = rand.Read(b)
	return now.UTC().Format("20060102T150405Z") + "-" + hex.EncodeToString(b)
}

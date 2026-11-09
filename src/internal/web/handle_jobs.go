package web

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ichava/icon-sets-sync/src/internal/core/sinks"
	"github.com/ichava/icon-sets-sync/src/internal/web/jobs"
)

// packProgress is one pack's state on the run page, rebuilt from the events
// so the page renders correctly without JavaScript.
type packProgress struct {
	Pack, Status, Detail, Err string
	Stages                    []stageProgress
}

type stageProgress struct {
	Pipeline, Stage, State, Err string
}

// reviewRecord is the result event a sync records for each review, so the
// list survives a restart with the rest of the run.
type reviewRecord struct {
	Kind   string       `json:"kind"`
	Pack   string       `json:"pack"`
	Review sinks.Review `json:"review"`
}

// reviewView is one review on the run page.
type reviewView struct {
	Pack, Target, Reason string
	Removals             []sinks.Removal
	Renamed, Removed     int
	// Duplicates are removals whose bytes stay at a path shipped today.
	Duplicates int
}

type jobData struct {
	// PacksDone and PacksTotal drive the overall progress bar.
	PacksDone, PacksTotal int
	// Finished is set on the reload after the run ended, for a toast.
	Finished bool
	Reviews  []reviewView
	// Force is the re-run with --force a run that needs review offers.
	Force    *forceRerun
	Info     jobs.Info
	Live     bool
	Packs    []packProgress
	Stages   []stageProgress
	Log      []jobs.Event
	LastID   int64
	Dropped  bool
	Duration time.Duration
}

// maxLogLines is how much of the log the page renders; the full log is the
// download.
const maxLogLines = 2000

func (s *Server) lookupJob(id string) (jobs.Info, []jobs.Event, *jobs.Job, error) {
	if j := s.jobs.Get(id); j != nil {
		return j.Info(), j.Events(), j, nil
	}
	if s.history == nil || !jobs.ValidID(id) {
		return jobs.Info{}, nil, nil, jobs.ErrNotFound
	}
	info, events, err := s.history.Load(id)
	return info, events, nil, err
}

func (s *Server) handleJob(w http.ResponseWriter, r *http.Request) {
	info, events, live, err := s.lookupJob(r.PathValue("id"))
	if err != nil {
		s.renderError(w, r, http.StatusNotFound, "No such run", "That run is not in this server's memory or its history.")
		return
	}
	d := jobData{Info: info, Live: live != nil && info.Status == jobs.StatusRunning, Duration: info.Duration(s.now())}
	byPack := map[string]int{}
	for _, e := range events {
		d.LastID = e.ID
		switch e.Type {
		case jobs.EventPackStart:
			byPack[e.Pack] = len(d.Packs)
			d.Packs = append(d.Packs, packProgress{Pack: e.Pack, Status: "running", Detail: e.Msg})
		case jobs.EventPackDone:
			i, ok := byPack[e.Pack]
			if !ok {
				byPack[e.Pack] = len(d.Packs)
				d.Packs = append(d.Packs, packProgress{Pack: e.Pack})
				i = len(d.Packs) - 1
			}
			d.Packs[i].Status, d.Packs[i].Err = e.Status, e.Err
			if e.Msg != "" {
				d.Packs[i].Detail = e.Msg
			}
		case jobs.EventStage:
			d.Stages = upsertStage(d.Stages, e)
		case jobs.EventLog:
			d.Log = append(d.Log, e)
		case jobs.EventResult:
			var rec reviewRecord
			if json.Unmarshal(e.Data, &rec) == nil && rec.Kind == "review" {
				rv := reviewView{Pack: rec.Pack, Target: rec.Review.Target, Reason: rec.Review.Reason, Removals: rec.Review.Removals}
				for _, rm := range rv.Removals {
					switch {
					case rm.RenamedTo != "":
						rv.Renamed++
					case rm.SameAs != "":
						rv.Duplicates++
					default:
						rv.Removed++
					}
				}
				d.Reviews = append(d.Reviews, rv)
			}
		}
	}
	if len(events) > 0 && events[0].ID > 1 {
		d.Dropped = true
	}
	d.PacksTotal = len(info.Packs)
	for _, p := range d.Packs {
		if p.Status != "running" && p.Status != "" {
			d.PacksDone++
		}
	}
	d.Finished = r.URL.Query().Get("finished") == "1" && !d.Live
	if len(d.Reviews) > 0 && !s.opts.ReadOnly {
		d.Force = newForceRerun(info, d.Reviews)
	}
	if len(d.Log) > maxLogLines {
		d.Log, d.Dropped = d.Log[len(d.Log)-maxLogLines:], true
	}
	v := s.newView(r, "jobs", "Run "+info.ID, d)
	s.render(w, http.StatusOK, "job", v)
}

func upsertStage(list []stageProgress, e jobs.Event) []stageProgress {
	for i := range list {
		if list[i].Pipeline == e.Pipeline && list[i].Stage == e.Stage {
			list[i].State, list[i].Err = e.State, e.Err
			return list
		}
	}
	return append(list, stageProgress{Pipeline: e.Pipeline, Stage: e.Stage, State: e.State, Err: e.Err})
}

type jobsData struct {
	Runs           []jobs.Info
	Pack, Outcome  string
	Packs          []string
	Now            time.Time
	HistoryEnabled bool
}

func (s *Server) handleJobs(w http.ResponseWriter, r *http.Request) {
	d := jobsData{Pack: r.URL.Query().Get("pack"), Outcome: r.URL.Query().Get("outcome"), Now: s.now(), HistoryEnabled: s.history != nil}
	seen := map[string]bool{}
	for _, info := range s.jobs.List() {
		for _, p := range info.Packs {
			if !seen[p] {
				seen[p] = true
				d.Packs = append(d.Packs, p)
			}
		}
		if d.Pack != "" && !contains(info.Packs, d.Pack) {
			continue
		}
		if d.Outcome != "" && string(info.Status) != d.Outcome {
			continue
		}
		d.Runs = append(d.Runs, info)
	}
	s.render(w, http.StatusOK, "jobs", s.newView(r, "jobs", "Runs", d))
}

func (s *Server) handleCancel(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !s.jobs.Cancel(id) {
		s.renderError(w, r, http.StatusNotFound, "No such run", "Only a run started by this server can be cancelled.")
		return
	}
	http.Redirect(w, r, "/jobs/"+id, http.StatusSeeOther)
}

// handleJobLog downloads a run as JSON Lines: the Info, then every event.
func (s *Server) handleJobLog(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !jobs.ValidID(id) {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("Content-Disposition", `attachment; filename="icon-sets-sync-`+id+`.jsonl"`)
	w.Header().Set("Cache-Control", "no-store")
	if j := s.jobs.Get(id); j != nil {
		enc := json.NewEncoder(w)
		_ = enc.Encode(j.Info())
		for _, e := range j.Events() {
			_ = enc.Encode(e)
		}
		return
	}
	if s.history == nil {
		http.NotFound(w, r)
		return
	}
	f, err := s.history.Open(id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer func() { _ = f.Close() }()
	_, _ = io.Copy(w, f)
}

// sseHeartbeat keeps proxies from closing an idle stream.
var sseHeartbeat = 15 * time.Second

// handleEvents streams a job's events as Server-Sent Events. It replays
// everything after Last-Event-ID (or ?after=), then tails the job until it
// ends. A finished or persisted job is replayed and the stream closed.
func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	after, _ := strconv.ParseInt(firstNonEmpty(r.Header.Get("Last-Event-ID"), r.URL.Query().Get("after")), 10, 64)

	flusher, _ := w.(http.Flusher)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")

	j := s.jobs.Get(id)
	if j == nil {
		_, events, _, err := s.lookupJob(id)
		if errors.Is(err, jobs.ErrNotFound) || err != nil {
			http.NotFound(w, r)
			return
		}
		for _, e := range events {
			if e.ID > after {
				writeEvent(w, e)
			}
		}
		if flusher != nil {
			flusher.Flush()
		}
		return
	}

	tick := time.NewTicker(sseHeartbeat)
	defer tick.Stop()
	for {
		events, done, changed := j.Since(after)
		for _, e := range events {
			writeEvent(w, e)
			after = e.ID
		}
		if flusher != nil {
			flusher.Flush()
		}
		if done {
			return
		}
		select {
		case <-changed:
		case <-tick.C:
			_, _ = io.WriteString(w, ": ping\n\n")
		case <-r.Context().Done():
			return
		case <-s.base.Done():
			return
		}
	}
}

func writeEvent(w io.Writer, e jobs.Event) {
	data, err := json.Marshal(e)
	if err != nil {
		data = []byte(`{"type":"log","msg":"unencodable event"}`)
	}
	_, _ = fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", e.ID, e.Type, data)
}

// forceRerun is the form that re-runs a run that needs review with --force.
type forceRerun struct {
	Action, Mode, Confirm string
	// Phrase and Packs feed the typed confirmation a real re-run needs.
	Phrase, Packs string
}

// maxConfirmNames caps the icons a confirm dialog names; the page above it
// lists every one.
const maxConfirmNames = 40

func newForceRerun(info jobs.Info, reviews []reviewView) *forceRerun {
	f := &forceRerun{Mode: "real"}
	if info.DryRun {
		f.Mode = "dry-run"
	}
	switch {
	case info.Kind == jobs.KindSync && len(info.Packs) == 1:
		f.Action, f.Phrase = "/packs/"+info.Packs[0]+"/sync", info.Packs[0]
	case info.Kind == jobs.KindSyncAll:
		f.Action, f.Phrase = "/sync-all", "all packs"
	default:
		return nil
	}
	var names []string
	total := 0
	for _, rv := range reviews {
		for _, rm := range rv.Removals {
			total++
			if len(names) < maxConfirmNames {
				if rm.RenamedTo != "" {
					names = append(names, rm.Path+" -> "+rm.RenamedTo)
				} else {
					names = append(names, rm.Path+" (removed)")
				}
			}
		}
	}
	what := "A dry run again, with the guard let through: it writes nothing."
	if f.Mode == "real" {
		what = "A real run that writes, commits and opens a pull request."
	}
	f.Packs = strings.Join(info.Packs, " ")
	f.Confirm = fmt.Sprintf("%s It lets %d icon(s) go: %s", what, total, strings.Join(names, ", "))
	if total > len(names) {
		f.Confirm += fmt.Sprintf(", and %d more listed on this page", total-len(names))
	}
	f.Confirm += "."
	return f
}

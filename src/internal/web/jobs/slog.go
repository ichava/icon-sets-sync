package jobs

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/ichava/icon-sets-sync/src/internal/core/pipeline"
)

// Handler returns a slog.Handler that records every line into the job, at
// level and above. Passed to a run as its logger, it is what keeps two
// concurrent runs' logs apart.
func Handler(j *Job, level slog.Level) slog.Handler {
	return &handler{job: j, level: level}
}

type handler struct {
	job   *Job
	level slog.Level
	attrs []slog.Attr
	group string
}

func (h *handler) Enabled(_ context.Context, l slog.Level) bool { return l >= h.level }

func (h *handler) Handle(_ context.Context, r slog.Record) error {
	attrs := map[string]string{}
	add := func(a slog.Attr) {
		key := a.Key
		if h.group != "" {
			key = h.group + "." + key
		}
		attrs[key] = a.Value.Resolve().String()
	}
	for _, a := range h.attrs {
		add(a)
	}
	r.Attrs(func(a slog.Attr) bool {
		add(a)
		return true
	})
	if len(attrs) == 0 {
		attrs = nil
	}
	// Stamped by the job's clock, not the record's, so every event in a job
	// shares one time source.
	h.job.append(Event{Type: EventLog, Level: strings.ToLower(r.Level.String()), Msg: r.Message, Attrs: attrs})
	return nil
}

func (h *handler) WithAttrs(as []slog.Attr) slog.Handler {
	c := *h
	c.attrs = append(append([]slog.Attr{}, h.attrs...), as...)
	return &c
}

func (h *handler) WithGroup(name string) slog.Handler {
	c := *h
	if c.group != "" {
		name = c.group + "." + name
	}
	c.group = name
	return &c
}

// Observer returns a pipeline.Observer that records stage events into j.
func Observer(j *Job) pipeline.Observer { return observer{j} }

type observer struct{ j *Job }

func (o observer) StageStarted(p, stage string) {
	o.j.append(Event{Type: EventStage, Pipeline: p, Stage: stage, State: StageStarted})
}

func (o observer) StageFinished(p, stage string, metrics map[string]any, err error) {
	e := Event{Type: EventStage, Pipeline: p, Stage: stage, State: StageFinished, Metrics: jsonSafe(metrics)}
	if err != nil {
		e.State, e.Err = StageFailed, err.Error()
	}
	o.j.append(e)
}

// jsonSafe keeps the metrics a JSON encoder can write; anything else is
// rendered as text rather than dropped.
func jsonSafe(m map[string]any) map[string]any {
	if len(m) == 0 {
		return nil
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		switch x := v.(type) {
		case nil, bool, string, int, int64, float64, map[string]any, []any, []string:
			out[k] = x
		default:
			out[k] = fmt.Sprint(x)
		}
	}
	return out
}

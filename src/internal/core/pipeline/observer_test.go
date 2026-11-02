package pipeline

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
)

type event struct {
	kind, pipeline, stage string
	err                   error
	metrics               map[string]any
}

type recordingObserver struct{ events []event }

func (o *recordingObserver) StageStarted(p, s string) {
	o.events = append(o.events, event{kind: "start", pipeline: p, stage: s})
}
func (o *recordingObserver) StageFinished(p, s string, m map[string]any, err error) {
	o.events = append(o.events, event{kind: "finish", pipeline: p, stage: s, err: err, metrics: m})
}

func (o *recordingObserver) trace() string {
	var b strings.Builder
	for _, e := range o.events {
		fmt.Fprintf(&b, "%s:%s ", e.kind, e.stage)
	}
	return strings.TrimSpace(b.String())
}

func TestObserverSeesEveryStageInOrder(t *testing.T) {
	obs := &recordingObserver{}
	fired := false
	result, err := Named("demo").
		Source(recordingSource{}).
		Transform(flaggingTransform{fired: &fired}).
		Sink(copyingSink{fired: new(bool), target: filepath.Join(t.TempDir(), "out.svg")}).
		Observer(obs).
		Run()
	if err != nil || !result.Success {
		t.Fatalf("result %+v, err %v", result, err)
	}
	want := "start:RecordingSource finish:RecordingSource start:FlaggingTransform finish:FlaggingTransform start:CopyingSink finish:CopyingSink"
	if got := obs.trace(); got != want {
		t.Fatalf("events\n got %s\nwant %s", got, want)
	}
	// The transform's metric is visible from its own finish event onwards.
	if _, ok := obs.events[3].metrics["flagging"]; !ok {
		t.Errorf("FlaggingTransform finished without its metric: %v", obs.events[3].metrics)
	}
	if obs.events[0].pipeline != "demo" {
		t.Errorf("pipeline name %q", obs.events[0].pipeline)
	}
}

func TestObserverSeesTheFailureAndNothingAfter(t *testing.T) {
	obs := &recordingObserver{}
	result, _ := Named("demo").
		Source(recordingSource{}).
		Transform(failingTransform{}).
		Sink(copyingSink{fired: new(bool), target: filepath.Join(t.TempDir(), "out.svg")}).
		Observer(obs).
		Run()
	if result.Success {
		t.Fatal("a failing transform succeeded")
	}
	if got, want := obs.trace(), "start:RecordingSource finish:RecordingSource start:FailingTransform finish:FailingTransform"; got != want {
		t.Fatalf("events\n got %s\nwant %s", got, want)
	}
	if last := obs.events[len(obs.events)-1]; last.err == nil || last.err.Error() != "boom" {
		t.Errorf("finish error %v", last.err)
	}
}

func TestACancelledStageIsNeverStarted(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	obs := &recordingObserver{}
	_, _ = Named("demo").Source(recordingSource{}).
		Sink(copyingSink{fired: new(bool), target: filepath.Join(t.TempDir(), "out.svg")}).
		Observer(obs).RunContext(ctx)
	if len(obs.events) != 0 {
		t.Fatalf("observer saw %s", obs.trace())
	}
}

type loggingSource struct{ SourceKind }

func (loggingSource) Name() string { return "LoggingSource" }
func (loggingSource) Execute(ctx *Context) error {
	ctx.Logger().Info("stage-line-from-the-source")
	return recordingSource{}.Execute(ctx)
}

// A per-run logger receives the stage's own lines as well as the pipeline's,
// and the process-wide default receives none of them. That is what keeps two
// concurrent runs' logs apart.
func TestAPerRunLoggerReceivesStageLinesAndTheDefaultDoesNot(t *testing.T) {
	var run, global bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&global, nil)))
	defer slog.SetDefault(prev)

	_, err := Named("demo").Source(loggingSource{}).
		Sink(copyingSink{fired: new(bool), target: filepath.Join(t.TempDir(), "out.svg")}).
		Logger(slog.New(slog.NewTextHandler(&run, nil))).
		Run()
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"pipeline starting", "stage=LoggingSource", "stage-line-from-the-source"} {
		if !strings.Contains(run.String(), want) {
			t.Errorf("the run's logger lacks %q:\n%s", want, run.String())
		}
	}
	if global.Len() != 0 {
		t.Errorf("slog.Default received the run's lines:\n%s", global.String())
	}
}

func TestContextLoggerFallsBackToTheDefault(t *testing.T) {
	var c *Context
	if c.Logger() != slog.Default() {
		t.Error("nil Context did not fall back to slog.Default()")
	}
}

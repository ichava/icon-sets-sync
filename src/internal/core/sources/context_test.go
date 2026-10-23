package sources

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/ichava/icon-sets-sync/src/internal/core/pipeline"
)

type runKey struct{}

// seenDownloader records the context a download received, then fails so the
// source stops before it tries to extract anything.
type seenDownloader struct{ seen []any }

func (d *seenDownloader) Download(ctx context.Context, _, _ string, _ bool) (string, error) {
	d.seen = append(d.seen, ctx.Value(runKey{}))
	return "", errors.New("offline")
}

type nopSink struct{ pipeline.SinkKind }

func (nopSink) Name() string                      { return "Nop" }
func (nopSink) Execute(_ *pipeline.Context) error { return nil }

func TestDownloadsUseTheRunContext(t *testing.T) {
	for name, src := range map[string]func(*seenDownloader) pipeline.Source{
		"GithubArchive": func(d *seenDownloader) pipeline.Source {
			return GithubArchive{Owner: "o", Repo: "r", Version: "1.0.0", HTTP: d}
		},
		"UnicodeCldr": func(d *seenDownloader) pipeline.Source {
			return UnicodeCldr{UnicodeVersion: "16.0", HTTP: d}
		},
	} {
		t.Run(name, func(t *testing.T) {
			d := &seenDownloader{}
			ctx := context.WithValue(context.Background(), runKey{}, "run-3")
			result, err := pipeline.Named("demo").Source(src(d)).Sink(nopSink{}).RunContext(ctx)
			if err != nil || result.Success {
				t.Fatalf("expected the fake download to fail the run: %+v %v", result, err)
			}
			if len(d.seen) != 1 || d.seen[0] != "run-3" {
				t.Fatalf("download contexts %v, want [run-3]", d.seen)
			}
		})
	}
}

// A source with no logger of its own writes to the run's logger, so a
// server's per-run log stream shows what each source fetched.
func TestSourcesLogToTheRunLogger(t *testing.T) {
	var buf bytes.Buffer
	d := &seenDownloader{}
	_, _ = pipeline.Named("demo").
		Source(GithubArchive{Owner: "o", Repo: "r", Version: "1.0.0", HTTP: d}).
		Sink(nopSink{}).
		Logger(slog.New(slog.NewTextHandler(&buf, nil))).
		Run()
	if !strings.Contains(buf.String(), "msg=downloading") {
		t.Fatalf("the run's logger lacks the source's line:\n%s", buf.String())
	}
}

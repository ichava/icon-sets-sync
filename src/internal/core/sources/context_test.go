package sources

import (
	"context"
	"errors"
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

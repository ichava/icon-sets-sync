package recipes

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/ichava/icon-sets-sync/src/internal/core/config"
)

type stageLog struct{ lines []string }

func (s *stageLog) StageStarted(_, stage string) { s.lines = append(s.lines, "start:"+stage) }
func (s *stageLog) StageFinished(_, stage string, _ map[string]any, err error) {
	line := "finish:" + stage
	if err != nil {
		line += ":err"
	}
	s.lines = append(s.lines, line)
}

// Build hands Options.Logger and Options.Observer to every pipeline it builds.
// An empty version makes NpmTarball fail before it reaches for npm, so the run
// is offline and still exercises a real stage.
func TestBuildWiresTheLoggerAndObserver(t *testing.T) {
	pack := &config.PackConfig{
		Name: "icon-sets-demo", Pack: "ichava/icon-sets-demo", PackRoot: t.TempDir(),
		Source: config.SourceConfig{Type: "npm", Package: "demo"},
		Sinks:  []config.SinkConfig{{Type: "filesystem", Root: t.TempDir()}},
	}
	var buf bytes.Buffer
	obs := &stageLog{}
	built, err := Build(pack, "", Options{DryRun: true, Logger: slog.New(slog.NewTextHandler(&buf, nil)), Observer: obs})
	if err != nil {
		t.Fatal(err)
	}
	if len(built) == 0 {
		t.Fatal("no pipelines built")
	}
	result, err := built[0].Run()
	if err != nil {
		t.Fatal(err)
	}
	if result.Success {
		t.Fatal("expected the empty version to fail the source")
	}
	if got := strings.Join(obs.lines, " "); got != "start:NpmTarball finish:NpmTarball:err" {
		t.Errorf("observer saw %q", got)
	}
	if !strings.Contains(buf.String(), "pipeline failed") {
		t.Errorf("the per-run logger did not receive the pipeline's lines:\n%s", buf.String())
	}
}

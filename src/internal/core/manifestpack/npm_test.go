package manifestpack

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// S7 at the second call site: the manifest pack's fetcher shells out to npm
// pack too, and must not run lifecycle scripts either.
func TestNpmFetcherPassesIgnoreScripts(t *testing.T) {
	bin := t.TempDir()
	argsFile := filepath.Join(t.TempDir(), "args")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >> \"$NPM_ARGS_FILE\"\nexit 1\n"
	if err := os.WriteFile(filepath.Join(bin, "npm"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("NPM_ARGS_FILE", argsFile)

	_, _ = NpmFetcher{}.Fetch(context.Background(), "bootstrap-icons@1.11.3", t.TempDir())

	got, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("the fake npm was never called: %v", err)
	}
	if !strings.Contains(string(got), "--ignore-scripts") {
		t.Fatalf("npm pack ran without --ignore-scripts; args:\n%s", got)
	}
}

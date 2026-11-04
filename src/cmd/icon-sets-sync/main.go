// Command icon-sets-sync keeps the Ichava icon packs in step with their
// upstream registries.
//
// Thin by design: it builds the cobra root and exits with whatever code the
// command returned. Everything else lives under internal/, so the CLI and the
// TUI stay two front-ends over one engine rather than two implementations.
//
// This is the only binary, the one sync-pack.yml builds. The short alias it
// once shipped beside was retired on 2026-11-04 (TestOnlyOneEntrypoint).
package main

import (
	"os"

	"github.com/ichava/icon-sets-sync/src/internal/cli"
)

func main() {
	os.Exit(cli.Execute())
}

// Command icon-sets-sync keeps the Ichava icon packs in step with their
// upstream registries.
//
// Thin by design: it builds the cobra root and exits with whatever code the
// command returned. Everything else lives under internal/, so the CLI and the
// TUI stay two front-ends over one engine rather than two implementations.
//
// This is the primary binary and the Docker ENTRYPOINT. cmd/imt builds the
// same program under its short name.
package main

import (
	"os"

	"github.com/ichava/maintainer-toolkit/src/internal/cli"
)

func main() {
	os.Exit(cli.Execute())
}

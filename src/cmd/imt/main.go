// Command imt is the short alias of icon-sets-sync, kept for interactive use
// and for anyone who installed it before the primary binary was renamed.
//
// It is the same program -- cli.Execute -- so the two cannot diverge. Install
// either; the Docker image ships both.
package main

import (
	"os"

	"github.com/ichava/icon-sets-sync/src/internal/cli"
)

func main() {
	os.Exit(cli.Execute())
}

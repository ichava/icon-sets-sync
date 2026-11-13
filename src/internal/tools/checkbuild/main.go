// Command checkbuild fails when build/ breaks the local build layout: one
// folder per OS, checksums.txt and the tracked inputs at the root, archives/
// after `make dist`, and no folder inside any of them. See
// ./internal/tools/buildlayout. Run by `make build-local`, `make cross` and
// `make dist` after they write:
//
//	go -C src run ./internal/tools/checkbuild -dir ../build -min 1
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/ichava/icon-sets-sync/src/internal/tools/buildlayout"
)

func main() {
	dir := flag.String("dir", "build", "the build directory to check")
	minEntries := flag.Int("min", 1, "fail when fewer entries than this were inspected")
	flag.Parse()

	n, problems, err := buildlayout.Check(*dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "checkbuild:", err)
		os.Exit(1)
	}
	if n < *minEntries {
		fmt.Fprintf(os.Stderr, "checkbuild: inspected %d entries in %s, expected at least %d\n", n, *dir, *minEntries)
		os.Exit(1)
	}
	if len(problems) > 0 {
		fmt.Fprintf(os.Stderr, "checkbuild: %s breaks the layout (build/{os}/ with flat binaries, nothing else):\n", *dir)
		for _, p := range problems {
			fmt.Fprintln(os.Stderr, "  "+p)
		}
		os.Exit(1)
	}
	fmt.Printf("checkbuild: %s holds the per-OS layout (%d entries)\n", *dir, n)
}

//go:build !windows

package main

import (
	"fmt"
	"os"
)

// main exists so every GOOS compiles. Elsewhere the double-click launchers
// are the macOS app bundle and the Linux .desktop entry.
func main() {
	fmt.Fprintln(os.Stderr, "icon-sets-sync-launcher is the Windows double-click launcher; run icon-sets-sync directly here.")
	os.Exit(2)
}

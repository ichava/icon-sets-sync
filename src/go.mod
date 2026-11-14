// The module lives in src/, not at the repository root, per the Go layout in
// /opensource/CLAUDE.md. GitHub's go-import meta tag has no subdir field, so Go
// falls back to its built-in rule -- repo root is the first three path elements
// and the rest is a subdirectory. Hence the /src suffix, and hence every
// release needs a second `src/vX.Y.Z` tag alongside `vX.Y.Z`.
//
//	go -C src build ./cmd/icon-sets-sync
//
// Dependencies match simtabi/repoclone, the house Go reference, so the CLI
// and TUI layers run the stack the rest of the estate already does.
module github.com/ichava/icon-sets-sync/src

go 1.26.0

// v0.1.0 to v0.1.2 were published from history rewritten on 2026-11-13, so the
// Go proxy and sum.golang.org serve code that git no longer holds. The next
// release is v0.1.3 (release-as in release-please-config.json).
retract [v0.1.0, v0.1.2] // published from history rewritten on 2026-11-13

require (
	github.com/charmbracelet/bubbles v1.0.0
	github.com/charmbracelet/bubbletea v1.3.10
	github.com/charmbracelet/lipgloss v1.1.0
	github.com/mattn/go-isatty v0.0.24
	github.com/modelcontextprotocol/go-sdk v1.8.0
	github.com/muesli/termenv v0.16.0
	github.com/santhosh-tekuri/jsonschema/v6 v6.0.3
	github.com/spf13/cobra v1.10.2
	github.com/spf13/pflag v1.1.0
	go.yaml.in/yaml/v3 v3.0.6
	golang.org/x/text v0.43.0
)

require (
	github.com/aymanbagabas/go-osc52/v2 v2.0.1 // indirect
	github.com/charmbracelet/colorprofile v0.4.1 // indirect
	github.com/charmbracelet/x/ansi v0.11.6 // indirect
	github.com/charmbracelet/x/cellbuf v0.0.15 // indirect
	github.com/charmbracelet/x/term v0.2.2 // indirect
	github.com/clipperhouse/displaywidth v0.9.0 // indirect
	github.com/clipperhouse/stringish v0.1.1 // indirect
	github.com/clipperhouse/uax29/v2 v2.5.0 // indirect
	github.com/erikgeiser/coninput v0.0.0-20211004153227-1c3628e74d0f // indirect
	github.com/google/jsonschema-go v0.4.3 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/lucasb-eyer/go-colorful v1.3.0 // indirect
	github.com/mattn/go-localereader v0.0.1 // indirect
	github.com/mattn/go-runewidth v0.0.19 // indirect
	github.com/muesli/ansi v0.0.0-20230316100256-276c6243b2f6 // indirect
	github.com/muesli/cancelreader v0.2.2 // indirect
	github.com/rivo/uniseg v0.4.7 // indirect
	github.com/segmentio/asm v1.1.3 // indirect
	github.com/segmentio/encoding v0.5.4 // indirect
	github.com/xo/terminfo v0.0.0-20220910002029-abceb7e1c41e // indirect
	github.com/yosida95/uritemplate/v3 v3.0.2 // indirect
	golang.org/x/oauth2 v0.35.0 // indirect
	golang.org/x/sync v0.24.0 // indirect
	golang.org/x/sys v0.41.0 // indirect
	golang.org/x/time v0.15.0 // indirect
)

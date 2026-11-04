package cli

import (
	"os"
	"runtime"

	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/ichava/icon-sets-sync/src/internal/ui"
)

// launchEnv is everything the browser decision reads, gathered in one value so
// a test can state a machine (a CI runner, a headless Linux box, a terminal on
// a laptop) instead of being one.
type launchEnv struct {
	goos      string
	getenv    func(string) string
	stdinTTY  bool
	stdoutTTY bool
}

// currentLaunchEnv reads the real process. A seam: tests replace it.
var currentLaunchEnv = func() launchEnv {
	return launchEnv{
		goos:      runtime.GOOS,
		getenv:    os.Getenv,
		stdinTTY:  isTTY(os.Stdin),
		stdoutTTY: isTTY(os.Stdout),
	}
}

func isTTY(f *os.File) bool {
	return isatty.IsTerminal(f.Fd()) || isatty.IsCygwinTerminal(f.Fd())
}

// Reasons browserAllowed gives for saying no. The non-terminal one is the
// wording the no-argument path has always printed before its help.
const (
	noBrowserCI       = "CI is set, so no browser is opened; printing help instead."
	noBrowserOptOut   = "ICHAVA_NO_BROWSER is set, so no browser is opened; printing help instead."
	noBrowserTerminal = "not a terminal, so there is nothing to drive the menu; printing help instead."
	noBrowserDisplay  = "no DISPLAY or WAYLAND_DISPLAY, so there is no desktop to open a browser on; printing help instead."
)

// browserAllowed decides whether this run may open a browser, and why not.
//
// The binary runs on GitHub Actions runners and servers as often as on a
// laptop, so the answer is no unless every sign points at a person at a
// desktop: no CI, no opt-out, a terminal on both stdin and stdout (a
// double-click in Finder or Explorer opens one), and on Linux and the BSDs a
// display server to open it on.
func browserAllowed(e launchEnv) (bool, string) {
	switch {
	case e.getenv("CI") != "":
		return false, noBrowserCI
	case truthy(e.getenv("ICHAVA_NO_BROWSER")):
		return false, noBrowserOptOut
	case !e.stdinTTY || !e.stdoutTTY:
		return false, noBrowserTerminal
	case e.goos != "darwin" && e.goos != "windows" &&
		e.getenv("DISPLAY") == "" && e.getenv("WAYLAND_DISPLAY") == "":
		return false, noBrowserDisplay
	}
	return true, ""
}

func truthy(v string) bool {
	switch v {
	case "", "0", "false", "FALSE", "False", "no", "off":
		return false
	}
	return true
}

// runDefault is the root command with nothing after it.
//
// No arguments at all, which is what a double-click gives, opens the web UI
// in the default browser. Anything else -- a flag with no subcommand -- keeps
// the earlier behaviour: the terminal menu, or help where nobody can drive
// it. Every subcommand has its own RunE and never comes here.
func runDefault(cmd *cobra.Command, args []string) error {
	if len(args) > 0 || anyFlagSet(cmd.Flags()) {
		return runMenu(cmd, args)
	}
	ok, reason := browserAllowed(currentLaunchEnv())
	if !ok {
		ui.Info("%s", reason)
		return cmd.Help()
	}
	return serveUI(cmd, defaultServeOptions(), true)
}

func anyFlagSet(fs *pflag.FlagSet) bool {
	set := false
	fs.Visit(func(*pflag.Flag) { set = true })
	return set
}

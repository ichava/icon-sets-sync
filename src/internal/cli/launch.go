package cli

import (
	"fmt"
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
	// exe is the path the process was started by, unresolved.
	exe string
	// consoleAlone is Windows' console holding this process only.
	consoleAlone bool
}

// currentLaunchEnv reads the real process. A seam: tests replace it.
var currentLaunchEnv = func() launchEnv {
	return launchEnv{
		goos:      runtime.GOOS,
		getenv:    os.Getenv,
		stdinTTY:  isTTY(os.Stdin),
		stdoutTTY: isTTY(os.Stdout),
		exe:       executable(),
		// Asked only when no argument was given, so the CLI pays nothing.
		consoleAlone: runtime.GOOS == "windows" && len(os.Args) == 1 && consoleAlone(),
	}
}

func executable() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	return exe
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
// double-click on the bare binary in Finder or Explorer opens one) or a
// desktop launch (the .app, the .desktop entry; see desktopLaunchOf), and on
// Linux and the BSDs a display server to open it on.
func browserAllowed(e launchEnv) (bool, string) {
	desktop, _ := desktopLaunchOf(e)
	switch {
	case e.getenv("CI") != "":
		return false, noBrowserCI
	case truthy(e.getenv("ICHAVA_NO_BROWSER")):
		return false, noBrowserOptOut
	case (!e.stdinTTY || !e.stdoutTTY) && !desktop:
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
//
// Started from the .app or by an Explorer double-click, it hands the server
// to a detached `serve` and exits, so no window is left behind; if that
// start fails it serves here instead.
func runDefault(cmd *cobra.Command, args []string) error {
	if len(args) > 0 || anyFlagSet(cmd.Flags()) {
		return runMenu(cmd, args)
	}
	env := currentLaunchEnv()
	ok, reason := browserAllowed(env)
	if !ok {
		ui.Info("%s", reason)
		return cmd.Help()
	}
	if handOff(cmd) {
		return nil
	}
	if _, detach := desktopLaunchOf(env); detach {
		err := relaunchServe()
		if err == nil {
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), "icon-sets-sync is starting in the background and will open in your browser.")
			return nil
		}
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "could not start in the background (%s); serving here\n", err)
	}
	return serveUI(cmd, defaultServeOptions(), false)
}

func anyFlagSet(fs *pflag.FlagSet) bool {
	set := false
	fs.Visit(func(*pflag.Flag) { set = true })
	return set
}

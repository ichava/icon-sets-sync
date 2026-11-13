package cli

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"time"

	"github.com/spf13/cobra"

	"github.com/ichava/icon-sets-sync/src/internal/app"
	"github.com/ichava/icon-sets-sync/src/internal/core/config"
	"github.com/ichava/icon-sets-sync/src/internal/core/confighistory"
	"github.com/ichava/icon-sets-sync/src/internal/core/gitx"
	"github.com/ichava/icon-sets-sync/src/internal/version"
	"github.com/ichava/icon-sets-sync/src/internal/web"
)

// Seams for tests: the sandbox and CI cannot always bind a socket, and a test
// must not open a browser. serveBrowser is the opener: openBrowser shells out
// to the platform's own (open, xdg-open, rundll32), so there is no dependency
// to add, and a test swaps in a fake that records the URL.
var (
	serveListen  web.ListenFunc = net.Listen
	serveBrowser                = openBrowser
)

// serveOptions are the serve command's flags.
type serveOptions struct {
	addr        string
	port        int
	allowRemote bool
	allowHosts  []string
	readOnly    bool
	noOpen      bool
	tokenFile   string
}

// defaultServeOptions is `serve` with no flags: loopback, the default port or
// the next free one.
func defaultServeOptions() serveOptions {
	return serveOptions{addr: "127.0.0.1", port: web.DefaultPort}
}

func newServeCommand() *cobra.Command {
	o := defaultServeOptions()

	cmd := &cobra.Command{
		Use:     "serve",
		Aliases: []string{"ui"},
		Short:   "Open the web UI on this machine",
		Long: `Open the web UI on this machine.

Listens on 127.0.0.1, port 7878 by default. If that port is taken it takes the
next free one and prints the address it actually bound; --port pins a port and
fails if it is taken. The printed address carries a one-time token that signs
the browser in; without it every page answers 401.

Opens the default browser at that address when run by a person at a desktop:
never when CI is set, when ICHAVA_NO_BROWSER=1, when stdin or stdout is not a
terminal (unless a desktop started it: the macOS app, or ICHAVA_DESKTOP_LAUNCH=1),
or on Linux with neither DISPLAY nor WAYLAND_DISPLAY. --no-open
turns it off. Running the binary with no arguments at all does the same as
plain serve.

A non-loopback --addr is refused unless --allow-remote is passed. Prefer an
SSH tunnel: ssh -L 7878:127.0.0.1:7878 <host>. Beyond loopback the server
terminates TLS itself: with api.tls_cert and api.tls_key, else with a
self-signed certificate it generates once, keeps in the state directory and
prints the fingerprint of. Plain HTTP there needs api.tls=off-behind-proxy,
for a TLS proxy in front.

The REST API (api.enabled), its event stream (events.stream) and webhooks
(webhooks.enabled) are off until their settings turn them on; see
docs/tools/api.md and docs/tools/webhooks.md.

Runs from the UI are dry runs unless you turn that off and confirm twice.
--read-only removes the run and edit actions entirely.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return serveUI(cmd, o, cmd.Flags().Changed("port"))
		},
	}

	f := cmd.Flags()
	f.StringVar(&o.addr, "addr", o.addr, "address to listen on, host or host:port (a port here pins it, like --port)")
	f.IntVar(&o.port, "port", o.port, "port to listen on; when not given and the default is taken, the next free port is used")
	f.BoolVar(&o.allowRemote, "allow-remote", false, "allow a non-loopback --addr (served over TLS unless api.tls=off-behind-proxy; prefer an SSH tunnel)")
	f.StringArrayVar(&o.allowHosts, "allow-host", nil, "extra Host header to accept with --allow-remote, e.g. a proxy's name (repeatable)")
	f.BoolVar(&o.readOnly, "read-only", false, "serve no route that runs a sync or edits a file")
	f.BoolVar(&o.noOpen, "no-open", false, "do not open a browser (also ICHAVA_NO_BROWSER=1)")
	f.StringVar(&o.tokenFile, "token-file", "", "also write the sign-in address to this file, mode 0600, for scripts")
	return cmd
}

// serveUI runs the web UI until the command's context is cancelled. It is
// both `serve` and the no-argument default, so the two cannot drift apart.
// portChanged says --port was given, which pins the port.
func serveUI(cmd *cobra.Command, o serveOptions, portChanged bool) error {
	host, pinnedPort, hasPort, err := splitAddr(o.addr)
	if err != nil {
		return usageErr("--addr %q: %s", o.addr, err)
	}
	pinned := hasPort || portChanged
	if !hasPort {
		pinnedPort = o.port
	}
	if pinnedPort < 0 || pinnedPort > 65535 {
		return usageErr("--port %d: want 0-65535", pinnedPort)
	}
	if !web.IsLoopback(host) && !o.allowRemote {
		return usageErr("--addr %s is reachable from other machines; pass --allow-remote to mean it, "+
			"or tunnel instead: ssh -L %d:127.0.0.1:%d <host>", host, web.DefaultPort, web.DefaultPort)
	}

	stateDir := web.DefaultStateDir(os.LookupEnv)
	tlsPlan, err := planTLS(host, state.run)
	if err != nil {
		return usageErr("%s", err)
	}
	ln, err := web.Listen(serveListen, host, pinnedPort, pinned, web.DefaultPortAttempts)
	if err != nil {
		return failure(err)
	}
	bound := ln.Addr().(*net.TCPAddr).Port
	tlsResult, err := tlsPlan.load(stateDir, host, time.Now())
	if err != nil {
		_ = ln.Close()
		return failure(err)
	}
	if tlsResult.Config != nil {
		ln = tls.NewListener(ln, tlsResult.Config)
	}
	integ, err := newIntegrations(stateDir, host, state.run)
	if err != nil {
		_ = ln.Close()
		return failure(err)
	}

	token := web.NewToken()
	cfgDir, cfgSource := configSource()
	eng := configEngine()
	eng.ConfigDir, eng.ConfigSource, eng.Git = cfgDir, cfgSource, gitx.New()
	// The global config starts initialised: written from what this machine
	// has unless a person edited it (config init says the same in the CLI).
	var initLine string
	if !o.readOnly && os.Getenv("CI") == "" {
		if res, err := eng.InitGlobal(false); err != nil {
			initLine = "warning: could not initialise the global config: " + err.Error()
		} else if res.Changed {
			initLine = fmt.Sprintf("global config: initialised %s from this machine (it was %s)", res.Path, res.State)
		}
	}
	srv, err := web.New(web.Options{
		Engine:       eng,
		Token:        token,
		AllowedHosts: web.AllowedHosts(host, bound, o.allowHosts),
		AllowRemote:  o.allowRemote,
		ReadOnly:     o.readOnly,
		Version:      version.Version(),
		BindAddr:     ln.Addr().String(),
		HistoryDir:   web.DefaultHistoryDir(os.LookupEnv),
		Bus:          integ.bus,
		TLS:          tlsResult.Config != nil,
		API:          integ.api,
	})
	if err != nil {
		_ = ln.Close()
		return failure(err)
	}

	scheme := "http"
	if tlsResult.Config != nil {
		scheme = "https"
	}
	origin := fmt.Sprintf("%s://%s", scheme, net.JoinHostPort(displayHost(host), strconv.Itoa(bound)))
	url := origin + "/?token=" + token
	out := cmd.OutOrStdout()
	if bound != pinnedPort && pinnedPort != 0 {
		_, _ = fmt.Fprintf(out, "port %d is in use; listening on %d instead\n", pinnedPort, bound)
	}
	_, _ = fmt.Fprintf(out, "config: %s (%s)\n", cfgDir, cfgSource)
	if initLine != "" {
		_, _ = fmt.Fprintln(out, initLine)
	}
	// A server started from a desktop shortcut sees a minimal PATH; say what
	// it is, and which tools a run would miss, before a run fails on it.
	slog.Info("PATH at startup", "path", os.Getenv("PATH"))
	for _, t := range app.MissingTools(app.Tools()) {
		_, _ = fmt.Fprintf(out, "warning: %s not found; it %s (run `icon-sets-sync doctor`)\n", t.Name, t.Why)
	}
	if cfgSource == config.SourceEmbedded {
		_, _ = fmt.Fprintln(out, "  the configs built into this binary, read-only; pass --config-dir to use and edit a checkout's")
	}
	_, _ = fmt.Fprintf(out, "icon-sets-sync web UI: %s\n", url)
	tlsPlan.describe(out, cmd.ErrOrStderr(), tlsResult)
	integ.describe(out, origin)
	if o.readOnly {
		_, _ = fmt.Fprintln(out, "read-only: runs and edits are disabled")
	}
	allowed, _ := browserAllowed(currentLaunchEnv())
	openIt := allowed && !o.noOpen && !g.noInput && !g.jsonOut
	if openIt {
		// Run from Finder or Explorer this is the Terminal window that came
		// with it, so say what the window is for.
		_, _ = fmt.Fprintln(out, "running in your browser; close this window or press Ctrl-C to stop")
	} else {
		_, _ = fmt.Fprintln(out, "press Ctrl-C to stop")
	}

	if o.tokenFile != "" {
		if err := writeTokenFile(o.tokenFile, url); err != nil {
			_ = ln.Close()
			return failure(err)
		}
	}
	// Recorded for a second launch to find, on this machine's loopback only,
	// and removed when this server stops (Ctrl-C, Stop server, a closed window).
	if web.IsLoopback(host) && !o.allowRemote {
		if err := writeInstanceState(url); err != nil {
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "could not record this server for a second launch to find (%s)\n", err)
		}
		defer removeInstanceState(url)
	}
	if openIt {
		if err := serveBrowser(url); err != nil {
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "could not open a browser (%s); open the address above yourself\n", err)
		}
	}

	ctx, stopIntegrations := context.WithCancel(cmd.Context())
	defer stopIntegrations()
	integ.start(ctx)
	if err := srv.Serve(cmd.Context(), ln); err != nil {
		return failure(err)
	}
	return nil
}

// configEngine is the engine the web UI and the config commands share, so a
// save from either keeps a copy in the same history and validates the same way.
func configEngine() *web.AppEngine {
	return &web.AppEngine{
		ConfigDir: configDir(), Workspace: workspace(),
		GlobalPath: state.globalPath, GlobalHome: configHome(), GlobalLocation: state.global, Stack: state.stack,
		History: confighistory.Store{Dir: confighistory.DefaultDir(os.LookupEnv), Keep: state.run.Int("history_keep")},
	}
}

// configHome is where a new global config is created: $XDG_CONFIG_HOME, else
// ~/.config, on every platform, per the house layout.
func configHome() string {
	if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
		return x
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config")
}

// splitAddr accepts "host" or "host:port".
func splitAddr(addr string) (host string, port int, hasPort bool, err error) {
	h, p, splitErr := net.SplitHostPort(addr)
	if splitErr != nil {
		if addr == "" {
			return "", 0, false, errors.New("empty address")
		}
		return addr, 0, false, nil
	}
	n, err := strconv.Atoi(p)
	if err != nil {
		return "", 0, false, fmt.Errorf("port %q is not a number", p)
	}
	return h, n, true, nil
}

// displayHost is the host to print: a wildcard bind is reachable here as
// 127.0.0.1, and that is one of the Host headers the server accepts.
func displayHost(host string) string {
	switch host {
	case "", "0.0.0.0", "::":
		return "127.0.0.1"
	}
	return host
}

func writeTokenFile(path, url string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintln(f, url); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	// O_CREATE's mode is not applied to a file that already existed.
	return os.Chmod(path, 0o600)
}

func openBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	return cmd.Start()
}

package cli

import (
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/ichava/icon-sets-sync/src/internal/core/gitx"
	"github.com/ichava/icon-sets-sync/src/internal/ui"
	"github.com/ichava/icon-sets-sync/src/internal/version"
	"github.com/ichava/icon-sets-sync/src/internal/web"
)

// Seams for tests: the sandbox and CI cannot always bind a socket, and a test
// must not open a browser.
var (
	serveListen  web.ListenFunc = net.Listen
	serveBrowser                = openBrowser
)

func newServeCommand() *cobra.Command {
	var (
		addr        string
		port        int
		allowRemote bool
		allowHosts  []string
		readOnly    bool
		noOpen      bool
		tokenFile   string
	)

	cmd := &cobra.Command{
		Use:     "serve",
		Aliases: []string{"ui"},
		Short:   "Open the web UI on this machine",
		Long: `Open the web UI on this machine.

Listens on 127.0.0.1, port 7878 by default. If that port is taken it takes the
next free one and prints the address it actually bound; --port pins a port and
fails if it is taken. The printed address carries a one-time token that signs
the browser in; without it every page answers 401.

A non-loopback --addr is refused unless --allow-remote is passed. Prefer an
SSH tunnel: ssh -L 7878:127.0.0.1:7878 <host>.

Runs from the UI are dry runs unless you turn that off and confirm twice.
--read-only removes the run and edit actions entirely.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			host, pinnedPort, hasPort, err := splitAddr(addr)
			if err != nil {
				return usageErr("--addr %q: %s", addr, err)
			}
			pinned := hasPort || cmd.Flags().Changed("port")
			if !hasPort {
				pinnedPort = port
			}
			if pinnedPort < 0 || pinnedPort > 65535 {
				return usageErr("--port %d: want 0-65535", pinnedPort)
			}
			if !web.IsLoopback(host) && !allowRemote {
				return usageErr("--addr %s is reachable from other machines; pass --allow-remote to mean it, "+
					"or tunnel instead: ssh -L %d:127.0.0.1:%d <host>", host, web.DefaultPort, web.DefaultPort)
			}

			ln, err := web.Listen(serveListen, host, pinnedPort, pinned, web.DefaultPortAttempts)
			if err != nil {
				return failure(err)
			}
			bound := ln.Addr().(*net.TCPAddr).Port

			token := web.NewToken()
			srv, err := web.New(web.Options{
				Engine: &web.AppEngine{
					ConfigDir: configDir(), Workspace: workspace(),
					GlobalPath: state.globalPath, Stack: state.stack, Git: gitx.New(),
				},
				Token:        token,
				AllowedHosts: web.AllowedHosts(host, bound, allowHosts),
				AllowRemote:  allowRemote,
				ReadOnly:     readOnly,
				Version:      version.Version(),
				BindAddr:     ln.Addr().String(),
				HistoryDir:   web.DefaultHistoryDir(os.LookupEnv),
			})
			if err != nil {
				_ = ln.Close()
				return failure(err)
			}

			url := fmt.Sprintf("http://%s/?token=%s", net.JoinHostPort(displayHost(host), strconv.Itoa(bound)), token)
			out := cmd.OutOrStdout()
			if bound != pinnedPort && pinnedPort != 0 {
				_, _ = fmt.Fprintf(out, "port %d is in use; listening on %d instead\n", pinnedPort, bound)
			}
			_, _ = fmt.Fprintf(out, "icon-sets-sync web UI: %s\n", url)
			if allowRemote {
				_, _ = fmt.Fprintln(cmd.ErrOrStderr(), "warning: --allow-remote serves plain HTTP; put a TLS proxy in front, or use an SSH tunnel")
			}
			if readOnly {
				_, _ = fmt.Fprintln(out, "read-only: runs and edits are disabled")
			}
			_, _ = fmt.Fprintln(out, "press Ctrl-C to stop")

			if tokenFile != "" {
				if err := writeTokenFile(tokenFile, url); err != nil {
					_ = ln.Close()
					return failure(err)
				}
			}
			if !noOpen && ui.Interactive() {
				if err := serveBrowser(url); err != nil {
					_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "could not open a browser (%s); open the address above yourself\n", err)
				}
			}

			if err := srv.Serve(cmd.Context(), ln); err != nil {
				return failure(err)
			}
			return nil
		},
	}

	f := cmd.Flags()
	f.StringVar(&addr, "addr", "127.0.0.1", "address to listen on, host or host:port (a port here pins it, like --port)")
	f.IntVar(&port, "port", web.DefaultPort, "port to listen on; when not given and the default is taken, the next free port is used")
	f.BoolVar(&allowRemote, "allow-remote", false, "allow a non-loopback --addr (plain HTTP; prefer an SSH tunnel)")
	f.StringArrayVar(&allowHosts, "allow-host", nil, "extra Host header to accept with --allow-remote, e.g. a proxy's name (repeatable)")
	f.BoolVar(&readOnly, "read-only", false, "serve no route that runs a sync or edits a file")
	f.BoolVar(&noOpen, "no-open", false, "do not open a browser")
	f.StringVar(&tokenFile, "token-file", "", "also write the sign-in address to this file, mode 0600, for scripts")
	return cmd
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

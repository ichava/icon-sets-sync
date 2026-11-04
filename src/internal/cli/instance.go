package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/ichava/icon-sets-sync/src/internal/web"
)

// instanceState is what a running server records about itself, so a second
// no-argument launch opens it instead of starting another. The URL carries
// the launch token, so the file is the owner's only (0600, in a 0700 dir).
type instanceState struct {
	URL string `json:"url"`
	PID int    `json:"pid"`
}

// stateFilePath is serve.json in the XDG state directory, beside the run
// history: $XDG_STATE_HOME/icon-sets-sync, else ~/.local/state/icon-sets-sync
// (%LocalAppData%\icon-sets-sync on Windows).
func stateFilePath() string {
	runs := web.DefaultHistoryDir(os.LookupEnv)
	if runs == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(runs), "serve.json")
}

func writeInstanceState(u string) error {
	path := stateFilePath()
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	b, err := json.Marshal(instanceState{URL: u, PID: os.Getpid()})
	if err != nil {
		return err
	}
	return writeTokenFileBytes(path, b)
}

func readInstanceState() (instanceState, bool) {
	var st instanceState
	path := stateFilePath()
	if path == "" {
		return st, false
	}
	b, err := os.ReadFile(path)
	if err != nil || json.Unmarshal(b, &st) != nil || st.URL == "" {
		return st, false
	}
	return st, true
}

// removeInstanceState removes the record only if it is still this server's,
// so a server stopping never deletes another one's.
func removeInstanceState(u string) {
	if st, ok := readInstanceState(); ok && st.URL == u {
		_ = os.Remove(stateFilePath())
	}
}

// probeInstance reports whether a server answers the health check at the
// recorded URL with its token. A seam: tests replace it.
var probeInstance = func(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "http" {
		return false
	}
	host := u.Hostname()
	if host != "127.0.0.1" && host != "::1" && host != "localhost" {
		// Only ever probe this machine with the token.
		return false
	}
	req, err := http.NewRequest(http.MethodGet, "http://"+u.Host+web.HealthPath, nil)
	if err != nil {
		return false
	}
	req.Header.Set(web.HealthTokenHeader, u.Query().Get("token"))
	client := &http.Client{
		Timeout:       2 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
	return resp.StatusCode == http.StatusOK && strings.TrimSpace(string(body)) == web.HealthBody
}

// handOff opens the browser at a live instance and reports true, or removes a
// stale record and reports false. It prints one line and never the token.
func handOff(cmd *cobra.Command) bool {
	st, ok := readInstanceState()
	if !ok {
		if path := stateFilePath(); path != "" {
			_ = os.Remove(path) // unreadable or empty: stale either way
		}
		return false
	}
	if !probeInstance(st.URL) {
		_ = os.Remove(stateFilePath())
		return false
	}
	if err := serveBrowser(st.URL); err != nil {
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "icon-sets-sync is already running, but no browser opened (%s); stop it, or open the address it printed\n", err)
		return true
	}
	_, _ = fmt.Fprintln(cmd.OutOrStdout(), "icon-sets-sync is already running; opened it in your browser.")
	return true
}

// writeTokenFileBytes writes b to path, mode 0600 even if it existed.
func writeTokenFileBytes(path string, b []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(b); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
}


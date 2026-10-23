package cli

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"testing"
	"time"
)

// blockingListener stands in for a bound socket: Accept blocks until Close,
// so http.Server.Serve runs and Shutdown ends it, with no port bound.
type blockingListener struct {
	addr   *net.TCPAddr
	closed chan struct{}
}

func (l *blockingListener) Accept() (net.Conn, error) {
	<-l.closed
	return nil, net.ErrClosed
}

func (l *blockingListener) Close() error {
	select {
	case <-l.closed:
	default:
		close(l.closed)
	}
	return nil
}

func (l *blockingListener) Addr() net.Addr { return l.addr }

// fakeListen refuses the taken ports with EADDRINUSE and "binds" the rest.
func fakeListen(t *testing.T, taken ...int) *[]string {
	t.Helper()
	var tried []string
	busy := map[int]bool{}
	for _, p := range taken {
		busy[p] = true
	}
	prev := serveListen
	serveListen = func(_, address string) (net.Listener, error) {
		tried = append(tried, address)
		host, port, _ := net.SplitHostPort(address)
		var p int
		_, _ = fmt.Sscan(port, &p)
		if busy[p] {
			return nil, &net.OpError{Op: "listen", Net: "tcp", Err: &os.SyscallError{Syscall: "bind", Err: syscall.EADDRINUSE}}
		}
		return &blockingListener{addr: &net.TCPAddr{IP: net.ParseIP(host), Port: p}, closed: make(chan struct{})}, nil
	}
	opened := serveBrowser
	serveBrowser = func(string) error { return errors.New("no browser in tests") }
	t.Cleanup(func() { serveListen, serveBrowser = prev, opened })
	return &tried
}

// runServe runs serve until it has printed its address, then stops it the way
// Ctrl-C would, by cancelling its context.
func runServe(t *testing.T, args ...string) (string, int) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	root := NewRootCommand()
	var out strings.Builder
	w := &notifyWriter{b: &out, seen: make(chan struct{}, 1)}
	root.SetOut(w)
	root.SetErr(w)
	root.SetArgs(append([]string{"--config-dir", filepath.Join("..", "..", "..", "config"), "serve", "--no-open"}, args...))
	done := make(chan int, 1)
	go func() {
		code := ExitOK
		if err := root.ExecuteContext(ctx); err != nil {
			var exit *ExitError
			if asExitError(err, &exit) {
				code = exit.Code
				if exit.Err != nil {
					w.writeString(exit.Err.Error())
				}
			} else {
				code = ExitFailure
			}
		}
		done <- code
	}()
	select {
	case <-w.seen:
		cancel()
	case code := <-done:
		cancel()
		return w.String(), code
	case <-time.After(10 * time.Second):
		cancel()
		t.Fatal("serve neither printed an address nor exited")
	}
	select {
	case code := <-done:
		return w.String(), code
	case <-time.After(15 * time.Second):
		t.Fatal("serve did not stop when its context was cancelled")
	}
	return "", 0
}

type notifyWriter struct {
	b    *strings.Builder
	seen chan struct{}
}

func (w *notifyWriter) Write(p []byte) (int, error) {
	w.writeString(string(p))
	return len(p), nil
}

func (w *notifyWriter) writeString(s string) {
	w.b.WriteString(s)
	if strings.Contains(s, "press Ctrl-C to stop") {
		select {
		case w.seen <- struct{}{}:
		default:
		}
	}
}

func (w *notifyWriter) String() string { return w.b.String() }

var printedURL = regexp.MustCompile(`http://127\.0\.0\.1:(\d+)/\?token=[A-Za-z0-9_-]{43}`)

func TestServeTakesTheNextFreePortAndSaysSo(t *testing.T) {
	isolate(t)
	tried := fakeListen(t, 7878)
	out, code := runServe(t)
	if code != ExitOK {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	m := printedURL.FindStringSubmatch(out)
	if m == nil || m[1] != "7879" {
		t.Fatalf("printed address is not the bound one:\n%s", out)
	}
	if !strings.Contains(out, "port 7878 is in use; listening on 7879 instead") {
		t.Errorf("the fallback was not announced:\n%s", out)
	}
	if strings.Join(*tried, " ") != "127.0.0.1:7878 127.0.0.1:7879" {
		t.Errorf("tried %v", *tried)
	}
}

func TestServePinnedPortFailsLoudly(t *testing.T) {
	isolate(t)
	tried := fakeListen(t, 9000)
	out, code := runServe(t, "--port", "9000")
	if code != ExitFailure || !strings.Contains(out, "port 9000 is already in use") {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	if len(*tried) != 1 {
		t.Errorf("a pinned port fell back: %v", *tried)
	}

	tried = fakeListen(t, 9001)
	if out, code := runServe(t, "--addr", "127.0.0.1:9001"); code != ExitFailure {
		t.Fatalf("a port in --addr did not pin: exit %d\n%s", code, out)
	}
	if len(*tried) != 1 {
		t.Errorf("tried %v", *tried)
	}
}

func TestServeRefusesARemoteBindWithoutAllowRemote(t *testing.T) {
	isolate(t)
	tried := fakeListen(t)
	out, code := runServe(t, "--addr", "0.0.0.0")
	if code != ExitUsage || !strings.Contains(out, "ssh -L 7878:127.0.0.1:7878") {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	if len(*tried) != 0 {
		t.Errorf("listened before refusing: %v", *tried)
	}

	out, code = runServe(t, "--addr", "0.0.0.0", "--allow-remote")
	if code != ExitOK || !strings.Contains(out, "plain HTTP") {
		t.Fatalf("--allow-remote: exit %d:\n%s", code, out)
	}
}

func TestServeWritesTheTokenFilePrivately(t *testing.T) {
	isolate(t)
	fakeListen(t)
	path := filepath.Join(t.TempDir(), "state", "serve.url")
	out, code := runServe(t, "--token-file", path)
	if code != ExitOK {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !printedURL.Match(body) {
		t.Errorf("token file holds %q", body)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0o600 {
		t.Errorf("token file mode %v", info.Mode().Perm())
	}
}

func TestSplitAddr(t *testing.T) {
	for in, want := range map[string]string{
		"127.0.0.1": "127.0.0.1 0 false", "127.0.0.1:9000": "127.0.0.1 9000 true",
		"[::1]:80": "::1 80 true", "localhost": "localhost 0 false",
	} {
		h, p, has, err := splitAddr(in)
		if err != nil || fmt.Sprintf("%s %d %v", h, p, has) != want {
			t.Errorf("splitAddr(%q) = %s %d %v %v", in, h, p, has, err)
		}
	}
	if _, _, _, err := splitAddr("127.0.0.1:http"); err == nil {
		t.Error("a non-numeric port was accepted")
	}
}

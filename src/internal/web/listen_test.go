package web

import (
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"syscall"
	"testing"
)

// fakeListener stands in for a bound socket: the sandbox this is developed in
// cannot bind one, and the logic under test is the choice of port.
type fakeListener struct{ addr *net.TCPAddr }

func (l fakeListener) Accept() (net.Conn, error) { return nil, errors.New("fake") }
func (l fakeListener) Close() error              { return nil }
func (l fakeListener) Addr() net.Addr            { return l.addr }

func inUse() error {
	return &net.OpError{Op: "listen", Net: "tcp", Err: &os.SyscallError{Syscall: "bind", Err: syscall.EADDRINUSE}}
}

// takenListen fails with EADDRINUSE for the listed ports and binds the rest.
func takenListen(taken map[int]bool, tried *[]string) ListenFunc {
	return func(_, address string) (net.Listener, error) {
		*tried = append(*tried, address)
		host, port, _ := net.SplitHostPort(address)
		var p int
		_, _ = fmt.Sscan(port, &p)
		if taken[p] {
			return nil, inUse()
		}
		return fakeListener{&net.TCPAddr{IP: net.ParseIP(host), Port: p}}, nil
	}
}

func TestListenFallsBackToTheNextFreePort(t *testing.T) {
	var tried []string
	ln, err := Listen(takenListen(map[int]bool{7878: true, 7879: true}, &tried), "127.0.0.1", 7878, false, DefaultPortAttempts)
	if err != nil {
		t.Fatal(err)
	}
	if got := ln.Addr().(*net.TCPAddr).Port; got != 7880 {
		t.Fatalf("bound %d, want 7880", got)
	}
	if strings.Join(tried, " ") != "127.0.0.1:7878 127.0.0.1:7879 127.0.0.1:7880" {
		t.Errorf("tried %v", tried)
	}
}

func TestListenPinnedFailsLoudly(t *testing.T) {
	var tried []string
	_, err := Listen(takenListen(map[int]bool{7878: true}, &tried), "127.0.0.1", 7878, true, DefaultPortAttempts)
	if err == nil {
		t.Fatal("a taken pinned port bound something")
	}
	if len(tried) != 1 || !strings.Contains(err.Error(), "port 7878 is already in use") {
		t.Errorf("tried %v, err %v", tried, err)
	}
	if !IsAddrInUse(err) {
		t.Error("the wrapped error lost EADDRINUSE")
	}
}

func TestListenGivesUpAfterTheAttempts(t *testing.T) {
	taken := map[int]bool{}
	for p := 7878; p < 7878+DefaultPortAttempts; p++ {
		taken[p] = true
	}
	var tried []string
	_, err := Listen(takenListen(taken, &tried), "127.0.0.1", 7878, false, DefaultPortAttempts)
	if err == nil || len(tried) != DefaultPortAttempts {
		t.Fatalf("tried %d, err %v", len(tried), err)
	}
}

func TestListenStopsAtAnErrorThatIsNotInUse(t *testing.T) {
	calls := 0
	_, err := Listen(func(string, string) (net.Listener, error) {
		calls++
		return nil, &net.OpError{Op: "listen", Err: &os.SyscallError{Syscall: "bind", Err: syscall.EACCES}}
	}, "127.0.0.1", 7878, false, DefaultPortAttempts)
	if err == nil || calls != 1 {
		t.Fatalf("calls %d, err %v", calls, err)
	}
}

func TestListenPortZeroAsksTheOS(t *testing.T) {
	var tried []string
	if _, err := Listen(takenListen(nil, &tried), "127.0.0.1", 0, false, DefaultPortAttempts); err != nil {
		t.Fatal(err)
	}
	if len(tried) != 1 || tried[0] != "127.0.0.1:0" {
		t.Errorf("tried %v", tried)
	}
}

func TestIsAddrInUseOnWindowsWording(t *testing.T) {
	if !IsAddrInUse(errors.New("listen tcp 127.0.0.1:7878: bind: Only one usage of each socket address (protocol/network address/port) is normally permitted.")) {
		t.Error("Windows wording not recognised")
	}
	if IsAddrInUse(errors.New("permission denied")) {
		t.Error("a permission error read as in use")
	}
}

func TestIsLoopback(t *testing.T) {
	for host, want := range map[string]bool{
		"127.0.0.1": true, "127.1.2.3": true, "::1": true, "[::1]": true, "localhost": true, "LocalHost": true,
		"0.0.0.0": false, "::": false, "192.168.1.5": false, "example.com": false, "": false,
	} {
		if IsLoopback(host) != want {
			t.Errorf("IsLoopback(%q) = %v", host, !want)
		}
	}
}

func TestAllowedHosts(t *testing.T) {
	got := strings.Join(AllowedHosts("0.0.0.0", 7880, []string{"sync.internal", "proxy:443"}), " ")
	want := "127.0.0.1:7880 localhost:7880 [::1]:7880 sync.internal:7880 proxy:443"
	if got != want {
		t.Errorf("got %s\nwant %s", got, want)
	}
	if got := AllowedHosts("10.0.0.5", 7878, nil); got[len(got)-1] != "10.0.0.5:7878" {
		t.Errorf("bound host missing: %v", got)
	}
}

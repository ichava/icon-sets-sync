package webhooks

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"strings"
	"syscall"
	"time"
)

// denied are the ranges a webhook may not reach unless webhooks.allow_private
// is on: the ones that lead into this machine, its LAN, or a cloud's
// metadata service rather than out to a receiver on the internet.
var denied = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),       // "this network", unspecified
	netip.MustParsePrefix("10.0.0.0/8"),      // RFC 1918
	netip.MustParsePrefix("100.64.0.0/10"),   // carrier-grade NAT
	netip.MustParsePrefix("127.0.0.0/8"),     // loopback
	netip.MustParsePrefix("169.254.0.0/16"),  // link-local, cloud metadata (169.254.169.254)
	netip.MustParsePrefix("172.16.0.0/12"),   // RFC 1918
	netip.MustParsePrefix("192.0.0.0/24"),    // IETF protocol assignments
	netip.MustParsePrefix("192.168.0.0/16"),  // RFC 1918
	netip.MustParsePrefix("198.18.0.0/15"),   // benchmarking
	netip.MustParsePrefix("224.0.0.0/4"),     // multicast
	netip.MustParsePrefix("240.0.0.0/4"),     // reserved, broadcast
	netip.MustParsePrefix("::/128"),          // unspecified
	netip.MustParsePrefix("::1/128"),         // loopback
	netip.MustParsePrefix("64:ff9b:1::/48"),  // local-use NAT64
	netip.MustParsePrefix("fc00::/7"),        // unique local (fd00:ec2::254 metadata)
	netip.MustParsePrefix("fe80::/10"),       // link-local
	netip.MustParsePrefix("ff00::/8"),        // multicast
	netip.MustParsePrefix("2001:db8::/32"),   // documentation
	netip.MustParsePrefix("100::/64"),        // discard
	netip.MustParsePrefix("2002::/16"),       // 6to4, which can embed any IPv4 address
	netip.MustParsePrefix("2001::/32"),       // Teredo, likewise
	netip.MustParsePrefix("::ffff:0:0/96"),   // IPv4-mapped; unmapped and checked as IPv4 too
	netip.MustParsePrefix("64:ff9b::/96"),    // NAT64, which can embed any IPv4 address
	netip.MustParsePrefix("::/96"),           // IPv4-compatible (deprecated)
	netip.MustParsePrefix("192.88.99.0/24"),  // 6to4 relay anycast
	netip.MustParsePrefix("198.51.100.0/24"), // documentation
	netip.MustParsePrefix("203.0.113.0/24"),  // documentation
	netip.MustParsePrefix("192.0.2.0/24"),    // documentation
}

// ErrDenied is a connection refused because of where it would go.
var ErrDenied = errors.New("webhook: address not allowed")

// Policy is what a delivery may reach.
type Policy struct {
	// AllowPrivate opens every denied range (webhooks.allow_private).
	AllowPrivate bool
	// AllowInsecureLocalhost allows http:// to localhost, 127.0.0.1 or ::1,
	// and only there (webhooks.allow_insecure_localhost).
	AllowInsecureLocalhost bool
}

// Denied reports whether addr is in a denied range.
func Denied(addr netip.Addr) bool {
	addr = addr.WithZone("")
	for _, p := range denied {
		if p.Contains(addr) {
			return true
		}
	}
	if addr.Is4In6() {
		return Denied(addr.Unmap())
	}
	return false
}

// isLocalhost is a URL host that names this machine.
func isLocalhost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	a, err := netip.ParseAddr(strings.Trim(host, "[]"))
	return err == nil && a.IsLoopback()
}

// CheckURL applies the scheme rule: https, or http to localhost with
// AllowInsecureLocalhost. It returns whether the delivery may connect to a
// loopback address, which only that exception allows without AllowPrivate.
func (p Policy) CheckURL(raw string) (loopbackOK bool, err error) {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return false, fmt.Errorf("webhook: %q is not a URL", raw)
	}
	if u.User != nil {
		return false, errors.New("webhook: a URL may not carry credentials")
	}
	switch u.Scheme {
	case "https":
		return false, nil
	case "http":
		if isLocalhost(u.Hostname()) && p.AllowInsecureLocalhost {
			return true, nil
		}
		if isLocalhost(u.Hostname()) {
			return false, errors.New("webhook: plain http is allowed only to localhost, and only with webhooks.allow_insecure_localhost")
		}
		return false, errors.New("webhook: plain http is refused; use https")
	}
	return false, fmt.Errorf("webhook: scheme %q is refused; use https", u.Scheme)
}

// control runs after the address is resolved and before the socket
// connects, so it sees the address actually dialled: a name that resolved
// to a public address for a check and to 169.254.169.254 a moment later (DNS
// rebinding) is caught here, where it matters.
func (p Policy) control(loopbackOK bool) func(network, address string, _ syscall.RawConn) error {
	return func(_, address string, _ syscall.RawConn) error {
		host, _, err := net.SplitHostPort(address)
		if err != nil {
			return fmt.Errorf("%w: %s", ErrDenied, address)
		}
		a, err := netip.ParseAddr(host)
		if err != nil {
			return fmt.Errorf("%w: %s", ErrDenied, address)
		}
		if p.AllowPrivate {
			return nil
		}
		if loopbackOK && a.Unmap().IsLoopback() {
			return nil
		}
		if Denied(a) {
			return fmt.Errorf("%w: %s is a private, loopback, link-local or reserved address (webhooks.allow_private opens it)", ErrDenied, a)
		}
		return nil
	}
}

// Dialer returns the guarded dialer for one delivery.
func (p Policy) Dialer(loopbackOK bool, timeout time.Duration) *net.Dialer {
	return &net.Dialer{Timeout: timeout, Control: p.control(loopbackOK)}
}

// DialContext is the guarded dial function for an http.Transport.
func (p Policy) DialContext(loopbackOK bool, timeout time.Duration) func(ctx context.Context, network, addr string) (net.Conn, error) {
	d := p.Dialer(loopbackOK, timeout)
	return d.DialContext
}

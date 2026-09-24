// Package netguard keeps experiment traffic away from addresses that are never
// an application under test, such as cloud metadata endpoints.
package netguard

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"syscall"
	"time"
)

// ErrBlocked is returned when a target resolves to a refused address.
var ErrBlocked = fmt.Errorf("netguard: address is not an experiment target")

var blockedHosts = map[string]struct{}{
	"metadata":                 {},
	"metadata.google.internal": {},
	"metadata.goog":            {},
}

// Blocked reports whether addr must never receive experiment traffic:
// link-local (169.254.0.0/16 and fe80::/10, where cloud metadata lives),
// multicast, and unspecified addresses. Loopback and private ranges stay
// allowed because that is where baseline and patch gateways run.
func Blocked(addr netip.Addr) bool {
	addr = addr.Unmap()
	return addr.IsLinkLocalUnicast() || addr.IsLinkLocalMulticast() ||
		addr.IsMulticast() || addr.IsUnspecified() || addr.IsInterfaceLocalMulticast()
}

// CheckURL rejects non-http(s) schemes, userinfo, known metadata hostnames,
// and literal blocked IPs. DNS names are checked again at dial time.
func CheckURL(raw string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return fmt.Errorf("netguard: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("netguard: target must be http or https")
	}
	if u.User != nil {
		return fmt.Errorf("netguard: target must not include userinfo")
	}
	host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	if host == "" {
		return fmt.Errorf("netguard: target missing host")
	}
	if _, bad := blockedHosts[host]; bad {
		return ErrBlocked
	}
	if addr, err := netip.ParseAddr(host); err == nil && Blocked(addr) {
		return ErrBlocked
	}
	return nil
}

// Transport returns an HTTP transport whose dialer refuses blocked addresses
// after DNS resolution, so hostnames and redirects cannot reach them.
func Transport() *http.Transport {
	d := &net.Dialer{
		Timeout: 5 * time.Second,
		Control: func(_, address string, _ syscall.RawConn) error {
			ap, err := netip.ParseAddrPort(address)
			if err != nil {
				return fmt.Errorf("netguard: %w", err)
			}
			if Blocked(ap.Addr()) {
				return ErrBlocked
			}
			return nil
		},
	}
	return &http.Transport{
		Proxy: nil,
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return d.DialContext(ctx, network, addr)
		},
		MaxIdleConns:        16,
		IdleConnTimeout:     30 * time.Second,
		TLSHandshakeTimeout: 5 * time.Second,
	}
}

package contentscan

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"syscall"
	"time"
)

// Worker safety (§5.3): a posted URL must not reveal the worker's network or
// reach its local services, and fetched material never touches disk. The
// fetcher refuses private, loopback and link-local destinations at dial time
// (after DNS resolution, so a rebinding hostname cannot slip through),
// follows at most a few redirects, and caps size and time. Egress through a
// VPN or proxy is a deployment concern: set HTTPS_PROXY and Go's transport
// honours it, or set FetchLimits.Proxy. A proxy then makes the outbound
// connection, so it must apply the same destination policy.

// FetchLimits bounds one fetch.
type FetchLimits struct {
	MaxBytes int64         // response body cap
	Timeout  time.Duration // whole-request timeout
	// AllowPrivate disables the destination guard (tests and local devnets
	// only).
	AllowPrivate bool
	// Proxy routes every fetch through this HTTP(S) proxy (e.g. a VPN
	// sidecar). Its own address is exempt from the destination guard.
	Proxy *url.URL
}

// DefaultFetchLimits: 16 MiB, 30 s.
var DefaultFetchLimits = FetchLimits{MaxBytes: 16 << 20, Timeout: 30 * time.Second}

// ErrTooLarge is returned when a body exceeds MaxBytes.
var ErrTooLarge = errors.New("contentscan: fetched body exceeds size limit")

// specialPurposeNets are IANA special-purpose ranges (RFC 6890 registries)
// that are not public unicast destinations but that net.IP's predicates
// don't cover: shared address space used by CGNAT and overlay VPNs such as
// Tailscale, "this network", benchmarking, IETF protocol assignments,
// documentation, reserved space, NAT64/IPv4-translated prefixes (which reach
// internal IPv4 hosts through a translator), Teredo, 6to4 and ORCHID.
var specialPurposeNets = func() []netip.Prefix {
	var out []netip.Prefix
	for _, c := range []string{
		"0.0.0.0/8",       // "this network"
		"100.64.0.0/10",   // shared address space (CGNAT, Tailscale)
		"192.0.0.0/24",    // IETF protocol assignments
		"192.0.2.0/24",    // TEST-NET-1
		"198.18.0.0/15",   // benchmarking
		"198.51.100.0/24", // TEST-NET-2
		"203.0.113.0/24",  // TEST-NET-3
		"240.0.0.0/4",     // reserved, incl. broadcast
		"64:ff9b::/96",    // NAT64 well-known prefix
		"64:ff9b:1::/48",  // local-use NAT64
		"100::/64",        // discard-only
		"2001::/32",       // Teredo
		"2001:2::/48",     // benchmarking
		"2001:10::/28",    // ORCHID
		"2001:20::/28",    // ORCHIDv2
		"2001:db8::/32",   // documentation
		"2002::/16",       // 6to4
		"::ffff:0:0/96",   // IPv4-mapped (checked again as IPv4 below)
		"::ffff:0:0:0/96", // IPv4-translated
		"fc00::/7",        // unique local (also IsPrivate)
		"fec0::/10",       // deprecated site-local
	} {
		out = append(out, netip.MustParsePrefix(c))
	}
	return out
}()

func disallowedIP(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsInterfaceLocalMulticast() || ip.IsMulticast() {
		return true
	}
	addr, ok := netip.AddrFromSlice(ip)
	if !ok {
		return true
	}
	if addr.Is4In6() {
		// An IPv4-mapped address is judged as the IPv4 address it maps.
		return disallowedIP(net.IP(addr.Unmap().AsSlice()))
	}
	for _, p := range specialPurposeNets {
		if p.Contains(addr) {
			return true
		}
	}
	return false
}

// NewFetchClient returns an http.Client enforcing the limits' destination
// guard.
func NewFetchClient(l FetchLimits) *http.Client {
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	proxyIPs := map[string]bool{}
	if l.Proxy != nil {
		if ips, err := net.LookupIP(l.Proxy.Hostname()); err == nil {
			for _, ip := range ips {
				proxyIPs[ip.String()] = true
			}
		}
	}
	if !l.AllowPrivate {
		dialer.Control = func(_, address string, _ syscall.RawConn) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return err
			}
			if proxyIPs[host] {
				return nil
			}
			if ip := net.ParseIP(host); ip == nil || disallowedIP(ip) {
				return fmt.Errorf("contentscan: refusing to connect to non-public address %s", host)
			}
			return nil
		}
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DialContext = dialer.DialContext
	if l.Proxy != nil {
		transport.Proxy = http.ProxyURL(l.Proxy)
	}
	return &http.Client{
		Timeout:   l.Timeout,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 3 {
				return errors.New("contentscan: too many redirects")
			}
			return checkScheme(req.URL, l.AllowPrivate)
		},
	}
}

func checkScheme(u *url.URL, allowPrivate bool) error {
	if u.Scheme == "https" || (allowPrivate && u.Scheme == "http") {
		return nil
	}
	return fmt.Errorf("contentscan: refusing scheme %q", u.Scheme)
}

// Fetch GETs rawURL into memory under the limits.
func Fetch(ctx context.Context, client *http.Client, rawURL string, l FetchLimits) ([]byte, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	if err := checkScheme(u, l.AllowPrivate); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("contentscan: GET %s: status %d", rawURL, resp.StatusCode)
	}
	return readCapped(resp.Body, l.MaxBytes)
}

func readCapped(r io.Reader, maxBytes int64) ([]byte, error) {
	if maxBytes <= 0 {
		maxBytes = DefaultFetchLimits.MaxBytes
	}
	b, err := io.ReadAll(io.LimitReader(r, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > maxBytes {
		return nil, ErrTooLarge
	}
	return b, nil
}

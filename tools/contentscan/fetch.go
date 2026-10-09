package contentscan

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
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

func disallowedIP(ip net.IP) bool {
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsInterfaceLocalMulticast() || ip.IsMulticast()
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

package contentscan

import (
	"net"
	"testing"
)

// CSC-03: the destination guard refuses every non-public range, including
// CGNAT (Tailscale), NAT64 and the IANA special-purpose blocks.
func TestDisallowedIP(t *testing.T) {
	blocked := []string{
		"127.0.0.1", "10.1.2.3", "172.16.0.1", "192.168.1.1", "169.254.1.1",
		"0.0.0.0", "0.1.2.3", "100.64.0.1", "100.100.100.100", "100.127.255.254",
		"192.0.0.8", "192.0.2.1", "198.18.0.1", "198.19.255.255", "198.51.100.7",
		"203.0.113.9", "240.0.0.1", "255.255.255.255", "224.0.0.1",
		"::1", "::", "fe80::1", "fc00::1", "fd12::1", "64:ff9b::a00:1", "64:ff9b:1::1",
		"2001:db8::1", "2001::1", "2002:a00:1::", "::ffff:10.0.0.1", "::ffff:100.64.0.1",
		"fec0::1", "ff02::1",
	}
	for _, s := range blocked {
		if !disallowedIP(net.ParseIP(s)) {
			t.Errorf("%s should be refused", s)
		}
	}
	allowed := []string{"1.1.1.1", "8.8.8.8", "100.63.255.255", "100.128.0.1", "198.20.0.1", "2606:4700::1111", "::ffff:8.8.8.8"}
	for _, s := range allowed {
		if disallowedIP(net.ParseIP(s)) {
			t.Errorf("%s should be allowed", s)
		}
	}
}

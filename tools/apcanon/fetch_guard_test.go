package apcanon

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The destination policy covers the special-purpose ranges the net.IP
// predicates miss (FED-07).
func TestIsDisallowedIPSpecialRanges(t *testing.T) {
	for _, s := range []string{
		"100.64.0.1", "100.127.255.254", "0.1.2.3", "198.18.0.1", "198.19.255.255",
		"64:ff9b::a9fe:a9fe", // NAT64 of 169.254.169.254
		"::ffff:127.0.0.1", "::ffff:169.254.169.254",
		"169.254.169.254", "127.0.0.1", "10.0.0.1", "::1", "fd00::1", "240.0.0.1",
	} {
		if !isDisallowedIP(net.ParseIP(s)) {
			t.Errorf("%s must be disallowed", s)
		}
	}
	for _, s := range []string{"1.1.1.1", "8.8.8.8", "2606:4700::1111"} {
		if isDisallowedIP(net.ParseIP(s)) {
			t.Errorf("%s must be allowed", s)
		}
	}
}

// DNS rebinding: even when the pre-flight host check is bypassed (a host
// that resolved public at check time and private at dial time), the
// transport refuses to connect to a non-public address.
func TestGuardedTransportRefusesPrivateDial(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	guarded := &http.Client{Transport: guardedTransport(false)}
	_, err := guarded.Get(srv.URL)
	if err == nil || !strings.Contains(err.Error(), "refusing to dial") {
		t.Fatalf("dial to %s must be refused, got %v", srv.URL, err)
	}

	open := &http.Client{Transport: guardedTransport(true)}
	resp, err := open.Get(srv.URL)
	if err != nil {
		t.Fatalf("AllowPrivateHosts transport must connect: %v", err)
	}
	resp.Body.Close()
}

// Environment proxies are ignored, so the dialed address is the target's.
func TestGuardedTransportIgnoresProxyEnv(t *testing.T) {
	if guardedTransport(false).Proxy != nil {
		t.Fatal("guarded transport must not use environment proxies")
	}
}

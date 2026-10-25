package crypto

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

func TestZkKeyFingerprint(t *testing.T) {
	if got := ZkKeyFingerprint(nil); got != "" {
		t.Fatalf("empty key: got %q", got)
	}
	key := make([]byte, 32)
	key[31] = 7
	got := ZkKeyFingerprint(key)
	sum := sha256.Sum256(key)
	h := hex.EncodeToString(sum[:8])
	want := h[0:4] + " " + h[4:8] + " " + h[8:12] + " " + h[12:16]
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	// Different keys never share a fingerprint in practice; at least these two don't.
	key2 := make([]byte, 32)
	key2[31] = 8
	if ZkKeyFingerprint(key2) == got {
		t.Fatal("distinct keys collided")
	}
}

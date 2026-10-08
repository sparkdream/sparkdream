package prover

import (
	"bytes"
	"testing"

	"sparkdream/tools/crypto"
)

// A secret key must be a canonical BN254 scalar: MiMC rejects larger values
// and HashToField drops that error, so a non-canonical key would derive a
// public key the circuit can never prove against.
func TestGenerateSecretKeyIsCanonical(t *testing.T) {
	var prev []byte
	for i := 0; i < 256; i++ {
		sk, err := GenerateSecretKey()
		if err != nil {
			t.Fatal(err)
		}
		if len(sk) != 32 {
			t.Fatalf("secret key is %d bytes, want 32", len(sk))
		}
		if !crypto.IsCanonicalFieldElement(sk) {
			t.Fatalf("secret key %x is not below the field modulus", sk)
		}
		if bytes.Equal(sk, prev) {
			t.Fatal("consecutive keys must differ")
		}
		prev = sk
	}
}

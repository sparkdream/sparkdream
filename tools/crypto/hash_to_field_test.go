package crypto

import (
	"bytes"
	"encoding/hex"
	"math/big"
	"testing"

	"github.com/consensys/gnark-crypto/ecc/bn254/fr"
	"github.com/consensys/gnark-crypto/ecc/bn254/fr/mimc"
)

// directMiMC hashes already-canonical 32-byte blocks with no reduction.
func directMiMC(t *testing.T, inputs ...[]byte) []byte {
	t.Helper()
	h := mimc.NewMiMC()
	for _, in := range inputs {
		if _, err := h.Write(PadTo32(in)); err != nil {
			t.Fatalf("mimc rejected canonical block %x: %v", in, err)
		}
	}
	return h.Sum(nil)
}

// TestHashToFieldReducesNonCanonical: a block >= r hashes as its value mod r
// (what the circuit sees), not as if it were omitted.
func TestHashToFieldReducesNonCanonical(t *testing.T) {
	big32 := bytes.Repeat([]byte{0xff}, 32)
	if IsCanonicalFieldElement(big32) {
		t.Fatal("0xff..ff must exceed the BN254 scalar modulus")
	}
	reduced := new(big.Int).Mod(new(big.Int).SetBytes(big32), fr.Modulus()).FillBytes(make([]byte, 32))
	other := []byte{7}

	if got, want := HashToField(big32, other), HashToField(reduced, other); !bytes.Equal(got, want) {
		t.Fatalf("non-canonical input not reduced mod r: %x != %x", got, want)
	}
	if bytes.Equal(HashToField(big32, other), HashToField(other)) {
		t.Fatal("non-canonical input was dropped from the hash")
	}
	if got, want := HashToField(big32), HashToField(reduced); !bytes.Equal(got, want) {
		t.Fatalf("single non-canonical input not reduced: %x != %x", got, want)
	}
	// r itself reduces to zero.
	r := fr.Modulus().FillBytes(make([]byte, 32))
	if got, want := HashToField(r), HashToField(make([]byte, 32)); !bytes.Equal(got, want) {
		t.Fatalf("r must hash as zero: %x != %x", got, want)
	}
}

// TestHashToFieldCanonicalUnchanged: canonical inputs (short, padded, and the
// largest canonical value r-1) hash exactly as raw MiMC over the padded block.
func TestHashToFieldCanonicalUnchanged(t *testing.T) {
	rMinus1 := new(big.Int).Sub(fr.Modulus(), big.NewInt(1)).FillBytes(make([]byte, 32))
	cases := [][][]byte{
		{},
		{{}},
		{{1}},
		{{1}, {2}},
		{[]byte("secret"), Uint64ToBytes(42)},
		{rMinus1},
		{rMinus1, {0}, bytes.Repeat([]byte{0x11}, 32)},
	}
	for i, in := range cases {
		if got, want := HashToField(in...), directMiMC(t, in...); !bytes.Equal(got, want) {
			t.Fatalf("case %d: HashToField %x != direct MiMC %x", i, got, want)
		}
	}
}

// TestHashToFieldGolden pins the canonical-input output so a change to
// padding or the hash is caught.
func TestHashToFieldGolden(t *testing.T) {
	got := hex.EncodeToString(HashToField([]byte{1}, []byte{2}))
	const want = "07f751d627280b8f73ebe288d68acd77dc2fd6962debda017df192e355065814"
	if got != want {
		t.Fatalf("HashToField(1, 2) = %s, want %s", got, want)
	}
}

// SHD-101: a group-tree (secret-ballot) rate-limit nullifier is bound to the
// ballot's scope, so one member's votes on two ballots in one shield epoch,
// and a vote and that member's trust-tree actions, carry different values.
func TestGroupTreeRateLimitNullifierIsPerBallot(t *testing.T) {
	sk := PadTo32([]byte{42})
	const epoch = 9
	trust := ComputeRateLimitNullifier(sk, epoch)
	p1 := ComputeRateLimitNullifierAt(sk, GroupTreeRateLimitEpoch(32, 1, epoch))
	p2 := ComputeRateLimitNullifierAt(sk, GroupTreeRateLimitEpoch(32, 2, epoch))
	poll := ComputeRateLimitNullifierAt(sk, GroupTreeRateLimitEpoch(33, 1, epoch))
	seen := map[string]bool{}
	for _, v := range [][]byte{trust, p1, p2, poll} {
		if seen[string(v)] {
			t.Fatal("two actions share a rate-limit nullifier")
		}
		seen[string(v)] = true
	}
	if !bytes.Equal(trust, ComputeRateLimitNullifierAt(sk, Uint64ToBytes(epoch))) {
		t.Fatal("ComputeRateLimitNullifierAt must match the uint64 form")
	}
	if !bytes.Equal(GroupTreeRateLimitEpoch(32, 1, epoch), GroupTreeRateLimitEpochForScope(ScopeElement(32, 1), epoch)) {
		t.Fatal("scope-element form differs")
	}
}

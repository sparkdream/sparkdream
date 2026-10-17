package tle

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/require"
	"go.dedis.ch/kyber/v4"
	"go.dedis.ch/kyber/v4/share"
	"go.dedis.ch/kyber/v4/util/random"
)

func TestEncryptDecryptRoundTrip(t *testing.T) {
	s := Suite.G1().Scalar().Pick(random.New())
	mpk := MasterPublicKeyG2(s)
	msg := []byte("anonymous ballot: yes on proposal 7")

	ct, err := Encrypt(mpk, 42, msg)
	require.NoError(t, err)
	require.Equal(t, Version, int(ct[0]))
	require.Len(t, ct, MinOverhead+len(msg))

	pt, err := Decrypt(EpochDecryptionKey(s, 42), 42, ct)
	require.NoError(t, err)
	require.Equal(t, msg, pt)

	// Two encryptions of the same message differ (fresh r and nonce).
	ct2, err := Encrypt(mpk, 42, msg)
	require.NoError(t, err)
	require.False(t, bytes.Equal(ct, ct2))
}

func TestDecryptRejectsWrongEpochKeyAndTamper(t *testing.T) {
	s := Suite.G1().Scalar().Pick(random.New())
	mpk := MasterPublicKeyG2(s)
	ct, err := Encrypt(mpk, 5, []byte("x"))
	require.NoError(t, err)

	// Epoch 6's key opens nothing encrypted to epoch 5 (forward secrecy).
	_, err = Decrypt(EpochDecryptionKey(s, 6), 5, ct)
	require.ErrorIs(t, err, ErrDecrypt)
	// The right key with the wrong epoch label fails too (epoch is in the KDF).
	_, err = Decrypt(EpochDecryptionKey(s, 5), 6, ct)
	require.ErrorIs(t, err, ErrDecrypt)
	// Another master secret.
	other := Suite.G1().Scalar().Pick(random.New())
	_, err = Decrypt(EpochDecryptionKey(other, 5), 5, ct)
	require.ErrorIs(t, err, ErrDecrypt)

	// Flip a ciphertext byte.
	bad := append([]byte(nil), ct...)
	bad[len(bad)-1] ^= 1
	_, err = Decrypt(EpochDecryptionKey(s, 5), 5, bad)
	require.ErrorIs(t, err, ErrDecrypt)
	// Flip a header byte (authenticated as additional data).
	bad = append([]byte(nil), ct...)
	bad[1+G2PointLen] ^= 1
	_, err = Decrypt(EpochDecryptionKey(s, 5), 5, bad)
	require.ErrorIs(t, err, ErrDecrypt)

	_, err = Decrypt(EpochDecryptionKey(s, 5), 5, ct[:MinOverhead-1])
	require.ErrorIs(t, err, ErrMalformed)
	bad = append([]byte(nil), ct...)
	bad[0] = 9
	_, err = Decrypt(EpochDecryptionKey(s, 5), 5, bad)
	require.ErrorIs(t, err, ErrMalformed)
}

func TestEncryptRejectsBadMasterKey(t *testing.T) {
	_, err := Encrypt([]byte("nope"), 1, []byte("m"))
	require.Error(t, err)
	nullG2, _ := Suite.G2().Point().Null().MarshalBinary()
	_, err = Encrypt(nullG2, 1, []byte("m"))
	require.Error(t, err)
	// A G1 point is not a G2 master key.
	_, err = Encrypt(MasterPublicKeyG1(Suite.G1().Scalar().Pick(random.New())), 1, []byte("m"))
	require.Error(t, err)
}

// TestThresholdShares runs the validator side: Shamir-share s, each validator
// derives its epoch share, shares verify against the G2 public shares, a
// threshold subset reconstructs the epoch key, and that key decrypts.
func TestThresholdShares(t *testing.T) {
	const n, threshold = 4, 3
	s := Suite.G1().Scalar().Pick(random.New())
	poly := share.NewPriPoly(Suite.G1(), threshold, s, random.New())
	priShares := poly.Shares(n)
	mpk := MasterPublicKeyG2(s)

	const epoch = 9
	ct, err := Encrypt(mpk, epoch, []byte("payload"))
	require.NoError(t, err)

	pubG2 := make([][]byte, n)
	epochShares := map[int][]byte{}
	for i, ps := range priShares {
		pubG2[i], _ = Suite.G2().Point().Mul(ps.V, nil).MarshalBinary()
		sh := DecryptionShare(ps.V, epoch)
		require.NoError(t, VerifyDecryptionShare(sh, pubG2[i], epoch), "share %d", i)
		// The same share claimed for another epoch fails.
		require.Error(t, VerifyDecryptionShare(sh, pubG2[i], epoch+1))
		// Another validator's public share does not vouch for it.
		if i > 0 {
			require.Error(t, VerifyDecryptionShare(sh, pubG2[0], epoch))
		}
		epochShares[ps.I+1] = sh
	}

	// Forged share: a random G1 point is well-formed but fails the pairing.
	forged, _ := Suite.G1().Point().Pick(random.New()).MarshalBinary()
	require.Error(t, VerifyDecryptionShare(forged, pubG2[0], epoch))

	// Any threshold-sized subset reconstructs the same key.
	subset := map[int][]byte{1: epochShares[1], 3: epochShares[3], 4: epochShares[4]}
	key, err := ReconstructEpochKey(subset, threshold, n)
	require.NoError(t, err)
	require.Equal(t, EpochDecryptionKey(s, epoch), key)
	pt, err := Decrypt(key, epoch, ct)
	require.NoError(t, err)
	require.Equal(t, []byte("payload"), pt)

	_, err = ReconstructEpochKey(map[int][]byte{1: epochShares[1]}, threshold, n)
	require.Error(t, err)
}

func TestExecFragmentRoundTrip(t *testing.T) {
	nullifier := bytes.Repeat([]byte{7}, 32)
	bz, err := EncodeExecFragment("/sparkdream.blog.v1.MsgCreatePost", []byte{1, 2, 3}, []byte("proof"), nullifier)
	require.NoError(t, err)
	url, val, proof, null, err := DecodeExecFragment(bz)
	require.NoError(t, err)
	require.Equal(t, "/sparkdream.blog.v1.MsgCreatePost", url)
	require.Equal(t, []byte{1, 2, 3}, val)
	require.Equal(t, []byte("proof"), proof)
	require.Equal(t, nullifier, null)

	// Proof and nullifier omitted: fields 3 and 4 absent.
	bz, err = EncodeExecFragment("/a.B", []byte{9}, nil, nil)
	require.NoError(t, err)
	_, _, proof, null, err = DecodeExecFragment(bz)
	require.NoError(t, err)
	require.Nil(t, proof)
	require.Nil(t, null)

	_, err = EncodeExecFragment("", nil, nil, nil)
	require.Error(t, err)
}

// TestEpochTagHasNoKnownDiscreteLog pins the fix for the epoch-tag bug: a tag
// built with Point().Pick(XOF(label)) is h·G1 with a public h, so an outsider
// holding only the G1 master public key could compute d = h·MPK and every
// validator's share h·pubShare. The tag must be a genuine hash-to-curve: the
// Pick-derived key must not open a ciphertext, and a Pick-derived "share" must
// fail verification.
func TestEpochTagHasNoKnownDiscreteLog(t *testing.T) {
	s := Suite.G1().Scalar().Pick(random.New())
	const epoch = 17
	ct, err := Encrypt(MasterPublicKeyG2(s), epoch, []byte("ballot"))
	require.NoError(t, err)

	// The attacker's best guess at the tag's discrete log: a scalar drawn
	// from an XOF of the same bytes the tag hashes, under both the current
	// domain and the legacy label.
	var be [8]byte
	binary.BigEndian.PutUint64(be[:], epoch)
	for _, label := range [][]byte{
		append([]byte(epochTagDomain), be[:]...),
		[]byte("shield_epoch_17"),
	} {
		h := Suite.G1().Scalar().Pick(g1Suite.XOF(label))
		require.False(t, EpochTag(epoch).Equal(Suite.G1().Point().Mul(h, nil)), "tag is a scalar multiple of the generator")
		mpk := Suite.G1().Point()
		require.NoError(t, mpk.UnmarshalBinary(MasterPublicKeyG1(s)))
		forgedKey, _ := Suite.G1().Point().Mul(h, mpk).MarshalBinary()
		_, err = Decrypt(forgedKey, epoch, ct)
		require.ErrorIs(t, err, ErrDecrypt)

		pubG2, _ := Suite.G2().Point().Mul(s, nil).MarshalBinary()
		forgedShare, _ := Suite.G1().Point().Mul(h, mpk).MarshalBinary()
		require.Error(t, VerifyDecryptionShare(forgedShare, pubG2, epoch))
	}

	// Determinism and epoch separation still hold.
	require.True(t, EpochTag(epoch).Equal(EpochTag(epoch)))
	require.False(t, EpochTag(epoch).Equal(EpochTag(epoch+1)))
	require.False(t, EpochTag(epoch).Equal(Suite.G1().Point().Null()))
}

var _ kyber.Scalar = Suite.G1().Scalar()

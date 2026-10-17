// Package tle is the threshold timelock encryption shared by the chain
// (x/shield decrypts), validators (decryption shares), seeding tools and web
// clients (encrypt). Everything that touches the ciphertext format or the key
// derivation lives here so the three sides cannot drift.
//
// Scheme: Boneh-Franklin identity-based encryption on BN256 (alt_bn128),
// where the identity is the shield epoch.
//
//	master secret      s            (Shamir-shared across validators by the DKG)
//	master public key  P2 = s·G2    (on chain: TLEKeySet.master_public_key_g2)
//	epoch tag          Q  = H1(epoch)              hash-to-G1
//	epoch key          d  = s·Q                    G1, reconstructed from shares
//	validator share    d_i = s_i·Q                 G1, one per validator
//
// Encrypt(P2, epoch, m): r random, U = r·G2, k = KDF(e(Q, P2)^r), c = AEAD(k, m).
// Decrypt(d, epoch, c):  k = KDF(e(d, U)) since e(s·Q, r·G2) = e(Q, s·G2)^r.
//
// A client needs only the master public key and the epoch number: no secret
// material is derivable from either. The epoch key d reveals nothing about
// other epochs' keys (the tags are independent hash outputs).
package tle

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"

	"go.dedis.ch/kyber/v4"
	"go.dedis.ch/kyber/v4/pairing/bn256"
	"go.dedis.ch/kyber/v4/share"
	"go.dedis.ch/kyber/v4/util/random"
)

// Format constants for the encrypted payload:
//
//	version(1) || U (G2 point, 128 bytes) || nonce (12) || AES-256-GCM(k, nonce, m)
const (
	Version     = 0x01
	g2PointLen  = 128
	nonceLen    = 12
	gcmTagLen   = 16
	headerLen   = 1 + g2PointLen + nonceLen
	MinOverhead = headerLen + gcmTagLen

	// G1PointLen / G2PointLen are the marshaled sizes kyber uses for BN256.
	G1PointLen = 64
	G2PointLen = g2PointLen

	kdfInfo = "sparkdream/shield/tle/v1"

	// epochTagDomain prefixes the epoch number hashed to the epoch tag.
	epochTagDomain = "sparkdream/shield/tle/epoch-tag/v1"
)

// hashablePoint is the hash-to-curve kyber's BN256 G1 point implements
// (try-and-increment over SHA-256; BN254 G1 has cofactor 1, so every curve
// point is in the group). It is deliberately not Point().Pick(XOF(...)):
// Pick draws a scalar from the stream and multiplies the generator, so the
// tag's discrete log would be public and d = s·Q = h·(s·G1) computable by
// anyone holding the G1 master public key.
type hashablePoint interface {
	Hash([]byte) kyber.Point
}

var (
	// Suite is the full BN256 pairing suite. G1 carries tags, shares and epoch
	// keys; G2 carries the master public key and the ciphertext's ephemeral
	// point.
	Suite = bn256.NewSuite()

	// g1Suite is the G1-only suite share.RecoverCommit interpolates over.
	g1Suite = bn256.NewSuiteG1()

	ErrMalformed = errors.New("tle: malformed ciphertext")
	ErrDecrypt   = errors.New("tle: decryption failed")
)

// EpochTag hashes a shield epoch to G1: Q = H1(epochTagDomain || be64(epoch)),
// a point with unknown discrete log. The DKG key store and every client
// derive tags through this function only.
func EpochTag(epoch uint64) kyber.Point {
	var be [8]byte
	binary.BigEndian.PutUint64(be[:], epoch)
	data := append([]byte(epochTagDomain), be[:]...)
	return Suite.G1().Point().(hashablePoint).Hash(data)
}

// EpochTagBytes is EpochTag marshaled.
func EpochTagBytes(epoch uint64) []byte {
	bz, _ := EpochTag(epoch).MarshalBinary()
	return bz
}

// MasterPublicKeyG2 is s·G2, what clients encrypt to.
func MasterPublicKeyG2(masterSecret kyber.Scalar) []byte {
	bz, _ := Suite.G2().Point().Mul(masterSecret, nil).MarshalBinary()
	return bz
}

// MasterPublicKeyG1 is s·G1, kept on chain for share bookkeeping.
func MasterPublicKeyG1(masterSecret kyber.Scalar) []byte {
	bz, _ := Suite.G1().Point().Mul(masterSecret, nil).MarshalBinary()
	return bz
}

// EpochDecryptionKey is d = s·H1(epoch). Only seeding tools and tests hold s;
// on a live chain d is reconstructed from validator shares.
func EpochDecryptionKey(masterSecret kyber.Scalar, epoch uint64) []byte {
	bz, _ := Suite.G1().Point().Mul(masterSecret, EpochTag(epoch)).MarshalBinary()
	return bz
}

// DecryptionShare is d_i = s_i·H1(epoch) for a validator's Shamir share s_i.
func DecryptionShare(secretShare kyber.Scalar, epoch uint64) []byte {
	bz, _ := Suite.G1().Point().Mul(secretShare, EpochTag(epoch)).MarshalBinary()
	return bz
}

// Encrypt produces a payload only the holder of epoch's decryption key can
// open. masterPublicKeyG2 is TLEKeySet.master_public_key_g2.
func Encrypt(masterPublicKeyG2 []byte, epoch uint64, plaintext []byte) ([]byte, error) {
	return encrypt(masterPublicKeyG2, epoch, plaintext, random.New())
}

func encrypt(masterPublicKeyG2 []byte, epoch uint64, plaintext []byte, rng cipher.Stream) ([]byte, error) {
	p2 := Suite.G2().Point()
	if err := p2.UnmarshalBinary(masterPublicKeyG2); err != nil {
		return nil, fmt.Errorf("tle: master public key is not a G2 point: %w", err)
	}
	if p2.Equal(Suite.G2().Point().Null()) {
		return nil, errors.New("tle: master public key is the identity")
	}

	r := Suite.G2().Scalar().Pick(rng)
	u := Suite.G2().Point().Mul(r, nil)
	uBytes, err := u.MarshalBinary()
	if err != nil {
		return nil, err
	}
	// e(Q, P2)^r
	gid := Suite.Pair(EpochTag(epoch), p2)
	shared := Suite.GT().Point().Mul(r, gid)

	key, err := deriveKey(epoch, uBytes, shared)
	if err != nil {
		return nil, err
	}
	aead, err := newAEAD(key)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, nonceLen)
	rng.XORKeyStream(nonce, nonce)

	out := make([]byte, 0, headerLen+len(plaintext)+gcmTagLen)
	out = append(out, Version)
	out = append(out, uBytes...)
	out = append(out, nonce...)
	out = aead.Seal(out, nonce, plaintext, out[:headerLen])
	return out, nil
}

// Decrypt opens a payload with the epoch decryption key d = s·H1(epoch).
func Decrypt(epochDecryptionKey []byte, epoch uint64, ciphertext []byte) ([]byte, error) {
	if len(ciphertext) < MinOverhead {
		return nil, ErrMalformed
	}
	if ciphertext[0] != Version {
		return nil, fmt.Errorf("%w: version %d", ErrMalformed, ciphertext[0])
	}
	d := Suite.G1().Point()
	if err := d.UnmarshalBinary(epochDecryptionKey); err != nil {
		return nil, fmt.Errorf("tle: decryption key is not a G1 point: %w", err)
	}
	uBytes := ciphertext[1 : 1+g2PointLen]
	u := Suite.G2().Point()
	if err := u.UnmarshalBinary(uBytes); err != nil {
		return nil, fmt.Errorf("%w: ephemeral point: %v", ErrMalformed, err)
	}
	// e(d, U) = e(s·Q, r·G2) = e(Q, s·G2)^r
	shared := Suite.Pair(d, u)
	key, err := deriveKey(epoch, uBytes, shared)
	if err != nil {
		return nil, err
	}
	aead, err := newAEAD(key)
	if err != nil {
		return nil, err
	}
	nonce := ciphertext[1+g2PointLen : headerLen]
	plaintext, err := aead.Open(nil, nonce, ciphertext[headerLen:], ciphertext[:headerLen])
	if err != nil {
		return nil, ErrDecrypt
	}
	return plaintext, nil
}

// VerifyDecryptionShare checks that share is secretShare·H1(epoch) for the
// validator whose G2 public share is publicShareG2:
//
//	e(share, G2) == e(H1(epoch), publicShareG2)
//
// A validator cannot produce a passing share for an epoch without its secret,
// and a wrong share (which would poison Lagrange reconstruction for everyone)
// is attributable to its submitter.
func VerifyDecryptionShare(shareBytes, publicShareG2 []byte, epoch uint64) error {
	sh := Suite.G1().Point()
	if err := sh.UnmarshalBinary(shareBytes); err != nil {
		return fmt.Errorf("tle: share is not a G1 point: %w", err)
	}
	if sh.Equal(Suite.G1().Point().Null()) {
		return errors.New("tle: share is the identity")
	}
	if len(publicShareG2) == 0 {
		return errors.New("tle: validator has no G2 public share")
	}
	pk := Suite.G2().Point()
	if err := pk.UnmarshalBinary(publicShareG2); err != nil {
		return fmt.Errorf("tle: public share is not a G2 point: %w", err)
	}
	if pk.Equal(Suite.G2().Point().Null()) {
		return errors.New("tle: public share is the identity")
	}
	lhs := Suite.Pair(sh, Suite.G2().Point().Base())
	rhs := Suite.Pair(EpochTag(epoch), pk)
	if !lhs.Equal(rhs) {
		return errors.New("tle: share does not match the validator's public share")
	}
	return nil
}

// VerifyG1G2Pair checks that a G1 point and a G2 point encode the same
// scalar: e(P1, G2) == e(G1, P2). Used for master keys and public shares,
// whose G2 halves are what clients and share verification rely on.
func VerifyG1G2Pair(p1Bytes, p2Bytes []byte) error {
	p1 := Suite.G1().Point()
	if err := p1.UnmarshalBinary(p1Bytes); err != nil {
		return fmt.Errorf("tle: not a G1 point: %w", err)
	}
	p2 := Suite.G2().Point()
	if err := p2.UnmarshalBinary(p2Bytes); err != nil {
		return fmt.Errorf("tle: not a G2 point: %w", err)
	}
	if p1.Equal(Suite.G1().Point().Null()) || p2.Equal(Suite.G2().Point().Null()) {
		return errors.New("tle: point is the identity")
	}
	if !Suite.Pair(p1, Suite.G2().Point().Base()).Equal(Suite.Pair(Suite.G1().Point().Base(), p2)) {
		return errors.New("tle: G1 and G2 points encode different scalars")
	}
	return nil
}

// ReconstructEpochKey interpolates threshold-many verified shares (1-based
// Shamir indices) into d = s·H1(epoch). n is the total number of shares the
// DKG issued.
func ReconstructEpochKey(shares map[int][]byte, threshold, n int) ([]byte, error) {
	if len(shares) < threshold {
		return nil, fmt.Errorf("tle: have %d shares, need %d", len(shares), threshold)
	}
	pub := make([]*share.PubShare, 0, len(shares))
	for idx, bz := range shares {
		p := Suite.G1().Point()
		if err := p.UnmarshalBinary(bz); err != nil {
			return nil, fmt.Errorf("tle: share %d: %w", idx, err)
		}
		pub = append(pub, &share.PubShare{I: idx - 1, V: p})
	}
	recovered, err := share.RecoverCommit(g1Suite, pub, threshold, n)
	if err != nil {
		return nil, fmt.Errorf("tle: interpolation: %w", err)
	}
	return recovered.MarshalBinary()
}

func deriveKey(epoch uint64, u []byte, shared kyber.Point) ([]byte, error) {
	gt, err := shared.MarshalBinary()
	if err != nil {
		return nil, err
	}
	var epochBytes [8]byte
	binary.BigEndian.PutUint64(epochBytes[:], epoch)
	info := append([]byte(kdfInfo), epochBytes[:]...)
	// salt = U binds the key to this ciphertext's ephemeral point.
	return hkdf.Key(sha256.New, gt, u, string(info), 32)
}

func newAEAD(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

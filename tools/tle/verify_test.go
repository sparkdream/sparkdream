package tle

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"
	"go.dedis.ch/kyber/v4/util/random"
)

func TestVerifyDecryptionShareRejects(t *testing.T) {
	s := Suite.G1().Scalar().Pick(random.New())
	pubG2 := MasterPublicKeyG2(s)
	share := DecryptionShare(s, 9)
	require.NoError(t, VerifyDecryptionShare(share, pubG2, 9))

	g1Null, err := Suite.G1().Point().Null().MarshalBinary()
	require.NoError(t, err)
	g2Null, err := Suite.G2().Point().Null().MarshalBinary()
	require.NoError(t, err)

	tests := []struct {
		name    string
		share   []byte
		pub     []byte
		epoch   uint64
		wantErr string
	}{
		{"share not a G1 point", []byte("short"), pubG2, 9, "share is not a G1 point"},
		{"empty share", nil, pubG2, 9, "share is not a G1 point"},
		{"identity share", g1Null, pubG2, 9, "share is the identity"},
		{"nil G2 public share", share, nil, 9, "no G2 public share"},
		{"empty G2 public share", share, []byte{}, 9, "no G2 public share"},
		{"G2 public share wrong size", share, []byte("short"), 9, "public share is not a G2 point"},
		{"identity G2 public share", share, g2Null, 9, "public share is the identity"},
		{"share for another epoch", share, pubG2, 10, "does not match"},
		{"another validator's public share", share, MasterPublicKeyG2(Suite.G1().Scalar().Pick(random.New())), 9, "does not match"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.ErrorContains(t, VerifyDecryptionShare(tt.share, tt.pub, tt.epoch), tt.wantErr)
		})
	}
}

func TestVerifyG1G2PairRejects(t *testing.T) {
	s := Suite.G1().Scalar().Pick(random.New())
	g1, g2 := MasterPublicKeyG1(s), MasterPublicKeyG2(s)
	require.NoError(t, VerifyG1G2Pair(g1, g2))

	g1Null, err := Suite.G1().Point().Null().MarshalBinary()
	require.NoError(t, err)
	g2Null, err := Suite.G2().Point().Null().MarshalBinary()
	require.NoError(t, err)
	other := Suite.G1().Scalar().Pick(random.New())

	tests := []struct {
		name    string
		p1, p2  []byte
		wantErr string
	}{
		{"G1 bytes wrong size", []byte("short"), g2, "not a G1 point"},
		{"G2 bytes wrong size", g1, []byte("short"), "not a G2 point"},
		{"G1 point passed as G2", g1, g1, "not a G2 point"},
		{"empty G2", g1, nil, "not a G2 point"},
		{"G2 bytes off the curve", g1, bytes.Repeat([]byte{0x01}, G2PointLen), "not a G2 point"},
		{"identity G1", g1Null, g2, "identity"},
		{"identity G2", g1, g2Null, "identity"},
		{"both identity", g1Null, g2Null, "identity"},
		{"different scalars", g1, MasterPublicKeyG2(other), "different scalars"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.ErrorContains(t, VerifyG1G2Pair(tt.p1, tt.p2), tt.wantErr)
		})
	}
}

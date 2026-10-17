package types_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.dedis.ch/kyber/v4/util/random"

	"sparkdream/tools/tle"
	"sparkdream/x/shield/types"
)

// TestTLEKeySetValidateShareEntries covers the per-share checks
// TestTLEKeySetValidate leaves out: nil entries and G1 public shares of the
// wrong size or not matching their G2 twin.
func TestTLEKeySetValidateShareEntries(t *testing.T) {
	pair := func() ([]byte, []byte) {
		s := tle.Suite.G1().Scalar().Pick(random.New())
		return tle.MasterPublicKeyG1(s), tle.MasterPublicKeyG2(s)
	}
	g1, g2 := pair()
	s1g1, s1g2 := pair()
	g1Null, err := tle.Suite.G1().Point().Null().MarshalBinary()
	require.NoError(t, err)
	g2Null, err := tle.Suite.G2().Point().Null().MarshalBinary()
	require.NoError(t, err)

	valid := func() types.TLEKeySet {
		return types.TLEKeySet{
			MasterPublicKey:      g1,
			MasterPublicKeyG2:    g2,
			ThresholdNumerator:   1,
			ThresholdDenominator: 1,
			ValidatorShares: []*types.TLEValidatorPublicShare{
				{ValidatorAddress: "zenith", PublicShare: s1g1, PublicShareG2: s1g2, ShareIndex: 1},
			},
		}
	}
	tests := []struct {
		name    string
		modify  func(*types.TLEKeySet)
		wantErr string
	}{
		{"nil validator share", func(ks *types.TLEKeySet) {
			ks.ValidatorShares = append(ks.ValidatorShares, nil)
		}, "nil validator share"},
		{"G1 public share too short", func(ks *types.TLEKeySet) {
			ks.ValidatorShares[0].PublicShare = s1g1[:types.G1PointLen-1]
		}, "public_share for zenith must be"},
		{"G1 public share too long", func(ks *types.TLEKeySet) {
			ks.ValidatorShares[0].PublicShare = append(append([]byte(nil), s1g1...), 0)
		}, "public_share for zenith must be"},
		{"G1 public share missing", func(ks *types.TLEKeySet) {
			ks.ValidatorShares[0].PublicShare = nil
		}, "public_share for zenith must be"},
		{"G2 public share wrong size", func(ks *types.TLEKeySet) {
			ks.ValidatorShares[0].PublicShareG2 = s1g2[:types.G2PointLen-1]
		}, "public_share_g2 for zenith"},
		{"G1 public share for another scalar", func(ks *types.TLEKeySet) {
			ks.ValidatorShares[0].PublicShare = g1
		}, "different scalars"},
		{"identity G1 public share", func(ks *types.TLEKeySet) {
			ks.ValidatorShares[0].PublicShare = g1Null
		}, "identity"},
		{"identity G2 public share", func(ks *types.TLEKeySet) {
			ks.ValidatorShares[0].PublicShareG2 = g2Null
		}, "identity"},
		{"identity G2 master key", func(ks *types.TLEKeySet) {
			ks.MasterPublicKeyG2 = g2Null
		}, "identity"},
		{"master key wrong size with no shares", func(ks *types.TLEKeySet) {
			ks.ValidatorShares = nil
			ks.MasterPublicKey = g1[:10]
		}, "master_public_key must"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ks := valid()
			require.NoError(t, ks.Validate())
			tt.modify(&ks)
			require.ErrorContains(t, ks.Validate(), tt.wantErr)
		})
	}
}

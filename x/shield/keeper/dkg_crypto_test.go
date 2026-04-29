package keeper

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/shield/types"
)

// makeValidG1Point generates a random valid BN256 G1 point and returns its bytes.
func makeValidG1Point() []byte {
	point := tleSuite.G1().Point().Pick(tleSuite.RandomStream())
	b, _ := point.MarshalBinary()
	return b
}

// makeValidCommitments generates n valid BN256 G1 points as commitments.
func makeValidCommitments(n int) [][]byte {
	cs := make([][]byte, n)
	for i := range cs {
		cs[i] = makeValidG1Point()
	}
	return cs
}

func TestValidateFeldmanCommitments(t *testing.T) {
	t.Run("valid commitments pass", func(t *testing.T) {
		cs := makeValidCommitments(3)
		err := ValidateFeldmanCommitments(cs, 3)
		require.NoError(t, err)
	})

	t.Run("wrong count rejected", func(t *testing.T) {
		cs := makeValidCommitments(2)
		err := ValidateFeldmanCommitments(cs, 3)
		require.Error(t, err)
		require.Contains(t, err.Error(), "expected 3 commitments, got 2")
	})

	t.Run("invalid G1 point rejected", func(t *testing.T) {
		cs := [][]byte{makeValidG1Point(), []byte("not a G1 point")}
		err := ValidateFeldmanCommitments(cs, 2)
		require.Error(t, err)
		require.Contains(t, err.Error(), "not a valid G1 point")
	})

	t.Run("identity element rejected", func(t *testing.T) {
		identity := tleSuite.G1().Point().Null()
		identityBytes, _ := identity.MarshalBinary()
		cs := [][]byte{makeValidG1Point(), identityBytes}
		err := ValidateFeldmanCommitments(cs, 2)
		require.Error(t, err)
		require.Contains(t, err.Error(), "identity element")
	})

	t.Run("empty commitments with zero expected passes", func(t *testing.T) {
		err := ValidateFeldmanCommitments(nil, 0)
		require.NoError(t, err)
	})
}

func TestAggregateFeldmanMasterKey(t *testing.T) {
	t.Run("no contributions returns error", func(t *testing.T) {
		_, err := aggregateFeldmanMasterKey(nil)
		require.Error(t, err)
		require.Contains(t, err.Error(), "no contributions")
	})

	t.Run("contribution with no commitments returns error", func(t *testing.T) {
		contributions := []types.DKGContribution{
			{ValidatorAddress: "val1", FeldmanCommitments: nil},
		}
		_, err := aggregateFeldmanMasterKey(contributions)
		require.Error(t, err)
		require.Contains(t, err.Error(), "no commitments")
	})

	t.Run("invalid C_0 returns error", func(t *testing.T) {
		contributions := []types.DKGContribution{
			{ValidatorAddress: "val1", FeldmanCommitments: [][]byte{[]byte("bad")}},
		}
		_, err := aggregateFeldmanMasterKey(contributions)
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid C_0")
	})

	t.Run("single contribution returns C_0 plus identity (which is C_0)", func(t *testing.T) {
		c0 := makeValidG1Point()
		contributions := []types.DKGContribution{
			{ValidatorAddress: "val1", FeldmanCommitments: [][]byte{c0, makeValidG1Point()}},
		}
		result, err := aggregateFeldmanMasterKey(contributions)
		require.NoError(t, err)
		require.NotEmpty(t, result)

		// Result should be a valid G1 point
		point := tleSuite.G1().Point()
		require.NoError(t, point.UnmarshalBinary(result))
	})

	t.Run("multiple contributions sum correctly", func(t *testing.T) {
		// Generate two known points
		s1 := tleSuite.Scalar().Pick(tleSuite.RandomStream())
		p1 := tleSuite.G1().Point().Mul(s1, nil) // p1 = s1 * G
		p1Bytes, _ := p1.MarshalBinary()

		s2 := tleSuite.Scalar().Pick(tleSuite.RandomStream())
		p2 := tleSuite.G1().Point().Mul(s2, nil) // p2 = s2 * G
		p2Bytes, _ := p2.MarshalBinary()

		contributions := []types.DKGContribution{
			{ValidatorAddress: "val1", FeldmanCommitments: [][]byte{p1Bytes}},
			{ValidatorAddress: "val2", FeldmanCommitments: [][]byte{p2Bytes}},
		}

		result, err := aggregateFeldmanMasterKey(contributions)
		require.NoError(t, err)

		// Expected: p1 + p2
		expected := tleSuite.G1().Point().Add(p1, p2)
		expectedBytes, _ := expected.MarshalBinary()
		require.Equal(t, expectedBytes, result)
	})
}

func TestComputePublicShareFromCommitments(t *testing.T) {
	t.Run("zero index returns error", func(t *testing.T) {
		_, err := computePublicShareFromCommitments(nil, 0)
		require.Error(t, err)
		require.Contains(t, err.Error(), "1-based")
	})

	t.Run("single contribution with constant polynomial", func(t *testing.T) {
		// Polynomial: f(x) = c0 (constant)
		// f(1) = c0, f(2) = c0, etc.
		c0 := makeValidG1Point()
		contributions := []types.DKGContribution{
			{ValidatorAddress: "val1", FeldmanCommitments: [][]byte{c0}},
		}

		result, err := computePublicShareFromCommitments(contributions, 1)
		require.NoError(t, err)
		require.Equal(t, c0, result, "constant polynomial should return C_0 at any index")
	})

	t.Run("produces valid G1 point", func(t *testing.T) {
		contributions := []types.DKGContribution{
			{ValidatorAddress: "val1", FeldmanCommitments: makeValidCommitments(2)},
			{ValidatorAddress: "val2", FeldmanCommitments: makeValidCommitments(2)},
		}

		result, err := computePublicShareFromCommitments(contributions, 1)
		require.NoError(t, err)
		require.NotEmpty(t, result)

		point := tleSuite.G1().Point()
		require.NoError(t, point.UnmarshalBinary(result))
	})

	t.Run("different indices produce different results", func(t *testing.T) {
		contributions := []types.DKGContribution{
			{ValidatorAddress: "val1", FeldmanCommitments: makeValidCommitments(2)},
		}

		result1, err := computePublicShareFromCommitments(contributions, 1)
		require.NoError(t, err)
		result2, err := computePublicShareFromCommitments(contributions, 2)
		require.NoError(t, err)

		// With a non-constant polynomial (degree >= 1), different indices should give different results
		require.NotEqual(t, result1, result2)
	})

	t.Run("invalid commitment in contribution returns error", func(t *testing.T) {
		contributions := []types.DKGContribution{
			{ValidatorAddress: "val1", FeldmanCommitments: [][]byte{[]byte("bad")}},
		}
		_, err := computePublicShareFromCommitments(contributions, 1)
		require.Error(t, err)
	})
}

func TestAggregateFeldmanDKGFromContributions(t *testing.T) {
	t.Run("empty contributions returns error", func(t *testing.T) {
		dkgState := types.DKGState{
			ExpectedValidators: []string{"val1"},
		}
		_, err := AggregateFeldmanDKGFromContributions(nil, dkgState)
		require.Error(t, err)
		require.Contains(t, err.Error(), "master key aggregation failed")
	})

	t.Run("successful aggregation", func(t *testing.T) {
		// Create contributions with valid G1 points for 2 validators, threshold polynomial degree 1 (2 commitments each)
		contributions := []types.DKGContribution{
			{ValidatorAddress: "val1", FeldmanCommitments: makeValidCommitments(2)},
			{ValidatorAddress: "val2", FeldmanCommitments: makeValidCommitments(2)},
		}
		dkgState := types.DKGState{
			ThresholdNumerator:   2,
			ThresholdDenominator: 3,
			ExpectedValidators:   []string{"val1", "val2"},
		}

		result, err := AggregateFeldmanDKGFromContributions(contributions, dkgState)
		require.NoError(t, err)
		require.NotNil(t, result)

		// Verify master key is valid
		require.NotEmpty(t, result.MasterPublicKey)
		mpk := tleSuite.G1().Point()
		require.NoError(t, mpk.UnmarshalBinary(result.MasterPublicKey))

		// Verify threshold is preserved
		require.Equal(t, uint64(2), result.ThresholdNumerator)
		require.Equal(t, uint64(3), result.ThresholdDenominator)

		// Verify we got public shares for all expected validators
		require.Len(t, result.ValidatorShares, 2)
		require.Equal(t, "val1", result.ValidatorShares[0].ValidatorAddress)
		require.Equal(t, uint32(1), result.ValidatorShares[0].ShareIndex)
		require.Equal(t, "val2", result.ValidatorShares[1].ValidatorAddress)
		require.Equal(t, uint32(2), result.ValidatorShares[1].ShareIndex)

		// Each share should be a valid G1 point
		for _, vs := range result.ValidatorShares {
			p := tleSuite.G1().Point()
			require.NoError(t, p.UnmarshalBinary(vs.PublicShare))
		}
	})

	t.Run("invalid commitment causes error", func(t *testing.T) {
		contributions := []types.DKGContribution{
			{ValidatorAddress: "val1", FeldmanCommitments: [][]byte{[]byte("bad")}},
		}
		dkgState := types.DKGState{
			ExpectedValidators: []string{"val1"},
		}

		_, err := AggregateFeldmanDKGFromContributions(contributions, dkgState)
		require.Error(t, err)
	})
}

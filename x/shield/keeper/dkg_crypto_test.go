package keeper

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.dedis.ch/kyber/v4"
	"go.dedis.ch/kyber/v4/pairing/bn256"

	"sparkdream/tools/tle"
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

// makeConsistentContribution builds a contribution whose G1 and G2 Feldman
// commitments encode the same random polynomial, and returns its constant
// term so tests can check the aggregated master keys.
func makeConsistentContribution(addr string, threshold int) (types.DKGContribution, kyber.Scalar) {
	g2 := bn256.NewSuiteG2()
	c := types.DKGContribution{ValidatorAddress: addr}
	var a0 kyber.Scalar
	for k := 0; k < threshold; k++ {
		a := tleSuite.Scalar().Pick(tleSuite.RandomStream())
		if k == 0 {
			a0 = a
		}
		g1, _ := tleSuite.G1().Point().Mul(a, nil).MarshalBinary()
		g2c, _ := g2.G2().Point().Mul(a, nil).MarshalBinary()
		c.FeldmanCommitments = append(c.FeldmanCommitments, g1)
		c.FeldmanCommitmentsG2 = append(c.FeldmanCommitmentsG2, g2c)
	}
	return c, a0
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
		// 2 validators, threshold polynomial degree 1 (2 commitments each),
		// G1 and G2 commitments from the same coefficients.
		c1, a1 := makeConsistentContribution("val1", 2)
		c2, a2 := makeConsistentContribution("val2", 2)
		contributions := []types.DKGContribution{c1, c2}
		dkgState := types.DKGState{
			ThresholdNumerator:   2,
			ThresholdDenominator: 3,
			ExpectedValidators:   []string{"val1", "val2"},
		}

		result, err := AggregateFeldmanDKGFromContributions(contributions, dkgState)
		require.NoError(t, err)
		require.NotNil(t, result)

		// Master keys: G1 and G2 encodings of s = a1 + a2.
		s := tleSuite.Scalar().Add(a1, a2)
		require.Equal(t, tle.MasterPublicKeyG1(s), result.MasterPublicKey)
		require.Equal(t, tle.MasterPublicKeyG2(s), result.MasterPublicKeyG2)

		// Verify threshold is preserved
		require.Equal(t, uint64(2), result.ThresholdNumerator)
		require.Equal(t, uint64(3), result.ThresholdDenominator)

		// Verify we got public shares for all expected validators
		require.Len(t, result.ValidatorShares, 2)
		require.Equal(t, "val1", result.ValidatorShares[0].ValidatorAddress)
		require.Equal(t, uint32(1), result.ValidatorShares[0].ShareIndex)
		require.Equal(t, "val2", result.ValidatorShares[1].ValidatorAddress)
		require.Equal(t, uint32(2), result.ValidatorShares[1].ShareIndex)

		// Each share is a G1 point with a matching G2 twin.
		g2 := bn256.NewSuiteG2()
		for _, vs := range result.ValidatorShares {
			p := tleSuite.G1().Point()
			require.NoError(t, p.UnmarshalBinary(vs.PublicShare))
			require.Len(t, vs.PublicShareG2, types.G2PointLen)
			q := g2.G2().Point()
			require.NoError(t, q.UnmarshalBinary(vs.PublicShareG2))
		}

		// The key set passes genesis validation and a client can encrypt to it.
		require.NoError(t, result.Validate())
		ct, err := tle.Encrypt(result.MasterPublicKeyG2, 1, []byte("m"))
		require.NoError(t, err)
		pt, err := tle.Decrypt(tle.EpochDecryptionKey(s, 1), 1, ct)
		require.NoError(t, err)
		require.Equal(t, []byte("m"), pt)
	})

	t.Run("missing G2 commitments rejected", func(t *testing.T) {
		contributions := []types.DKGContribution{
			{ValidatorAddress: "val1", FeldmanCommitments: makeValidCommitments(2)},
		}
		dkgState := types.DKGState{ThresholdNumerator: 1, ThresholdDenominator: 1, ExpectedValidators: []string{"val1"}}
		_, err := AggregateFeldmanDKGFromContributions(contributions, dkgState)
		require.Error(t, err)
		require.Contains(t, err.Error(), "G2")
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

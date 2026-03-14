package keeper

import (
	"fmt"

	"go.dedis.ch/kyber/v4"

	"sparkdream/x/shield/types"
)

// validateFeldmanCommitments checks that each commitment is a valid BN256 G1 point.
func validateFeldmanCommitments(commitments [][]byte, expectedCount int) error {
	if len(commitments) != expectedCount {
		return fmt.Errorf("expected %d commitments, got %d", expectedCount, len(commitments))
	}
	for i, c := range commitments {
		point := tleSuite.G1().Point()
		if err := point.UnmarshalBinary(c); err != nil {
			return fmt.Errorf("commitment %d is not a valid G1 point: %w", i, err)
		}
		if point.Equal(tleSuite.G1().Point().Null()) {
			return fmt.Errorf("commitment %d is the identity element", i)
		}
	}
	return nil
}

// aggregateFeldmanMasterKey computes the master public key from all contributions.
// MPK = Σ_i C_{i,0} (sum of all constant-term commitments).
func aggregateFeldmanMasterKey(contributions []types.DKGContribution) ([]byte, error) {
	if len(contributions) == 0 {
		return nil, fmt.Errorf("no contributions")
	}

	sum := tleSuite.G1().Point().Null()
	for _, c := range contributions {
		if len(c.FeldmanCommitments) == 0 {
			return nil, fmt.Errorf("contribution from %s has no commitments", c.ValidatorAddress)
		}
		point := tleSuite.G1().Point()
		if err := point.UnmarshalBinary(c.FeldmanCommitments[0]); err != nil {
			return nil, fmt.Errorf("invalid C_0 from %s: %w", c.ValidatorAddress, err)
		}
		sum = sum.Add(sum, point)
	}

	return sum.MarshalBinary()
}

// computePublicShareFromCommitments computes the aggregate public share for validator at 1-based index j.
// PubShare_j = Σ_i Eval(C_i, j) where Eval(C_i, j) = Σ_k C_{i,k} * j^k
func computePublicShareFromCommitments(contributions []types.DKGContribution, j uint32) ([]byte, error) {
	if j == 0 {
		return nil, fmt.Errorf("share index must be 1-based, got 0")
	}

	sum := tleSuite.G1().Point().Null()

	for _, c := range contributions {
		evalPoint, err := evalCommitmentsAtIndex(c.FeldmanCommitments, j)
		if err != nil {
			return nil, fmt.Errorf("eval failed for contribution from %s: %w", c.ValidatorAddress, err)
		}
		sum = sum.Add(sum, evalPoint)
	}

	return sum.MarshalBinary()
}

// evalCommitmentsAtIndex evaluates Feldman commitments at 1-based index j.
// Returns Σ_k (C_k * j^k) as a G1 point.
func evalCommitmentsAtIndex(commitments [][]byte, j uint32) (kyber.Point, error) {
	result := tleSuite.G1().Point().Null()
	jScalar := tleSuite.Scalar().SetInt64(int64(j))
	jPower := tleSuite.Scalar().One() // j^0 = 1

	for _, cBytes := range commitments {
		point := tleSuite.G1().Point()
		if err := point.UnmarshalBinary(cBytes); err != nil {
			return nil, fmt.Errorf("invalid commitment: %w", err)
		}
		// term = C_k * j^k
		term := tleSuite.G1().Point().Mul(jPower, point)
		result = result.Add(result, term)

		// j^(k+1) = j^k * j
		jPower = jPower.Mul(jPower, jScalar)
	}

	return result, nil
}

// AggregateFeldmanDKGFromContributions computes TLEKeySet from all DKG contributions.
// It sums all constant-term commitments to get the master public key and evaluates
// the aggregated polynomial at each validator's index to get their public share.
func AggregateFeldmanDKGFromContributions(contributions []types.DKGContribution, dkgState types.DKGState) (*types.TLEKeySet, error) {
	masterKey, err := aggregateFeldmanMasterKey(contributions)
	if err != nil {
		return nil, fmt.Errorf("master key aggregation failed: %w", err)
	}

	var valShares []*types.TLEValidatorPublicShare
	for i, valAddr := range dkgState.ExpectedValidators {
		idx := uint32(i + 1)
		pubShare, err := computePublicShareFromCommitments(contributions, idx)
		if err != nil {
			return nil, fmt.Errorf("public share computation failed for %s: %w", valAddr, err)
		}
		valShares = append(valShares, &types.TLEValidatorPublicShare{
			ValidatorAddress: valAddr,
			PublicShare:      pubShare,
			ShareIndex:       idx,
		})
	}

	return &types.TLEKeySet{
		MasterPublicKey:      masterKey,
		ThresholdNumerator:   dkgState.ThresholdNumerator,
		ThresholdDenominator: dkgState.ThresholdDenominator,
		ValidatorShares:      valShares,
	}, nil
}

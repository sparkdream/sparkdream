package keeper

import (
	"fmt"

	"go.dedis.ch/kyber/v4"
	"go.dedis.ch/kyber/v4/pairing/bn256"

	"sparkdream/x/shield/types"
)

// dkgG2Suite is the BN256 G2 suite used for G2 commitment operations during DKG.
var dkgG2Suite = bn256.NewSuiteG2()

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

// validateFeldmanCommitmentsG2 checks that each G2 commitment is a valid BN256 G2 point.
func validateFeldmanCommitmentsG2(commitments [][]byte, expectedCount int) error {
	if len(commitments) != expectedCount {
		return fmt.Errorf("expected %d G2 commitments, got %d", expectedCount, len(commitments))
	}
	for i, c := range commitments {
		point := dkgG2Suite.G2().Point()
		if err := point.UnmarshalBinary(c); err != nil {
			return fmt.Errorf("G2 commitment %d is not a valid G2 point: %w", i, err)
		}
		if point.Equal(dkgG2Suite.G2().Point().Null()) {
			return fmt.Errorf("G2 commitment %d is the identity element", i)
		}
	}
	return nil
}

// validateFeldmanCommitmentsConsistency verifies G1 and G2 commitments encode the same
// scalars using the pairing check: e(C_k_G1, G2_gen) == e(G1_gen, C_k_G2) for each k.
func validateFeldmanCommitmentsConsistency(g1Commitments, g2Commitments [][]byte) error {
	if len(g1Commitments) != len(g2Commitments) {
		return fmt.Errorf("commitment count mismatch: %d G1, %d G2", len(g1Commitments), len(g2Commitments))
	}
	pairingSuite := bn256.NewSuite()
	g1Gen := tleSuite.G1().Point().Base()
	g2Gen := dkgG2Suite.G2().Point().Base()

	for k := range g1Commitments {
		cG1 := tleSuite.G1().Point()
		if err := cG1.UnmarshalBinary(g1Commitments[k]); err != nil {
			return fmt.Errorf("invalid G1 commitment %d: %w", k, err)
		}
		cG2 := dkgG2Suite.G2().Point()
		if err := cG2.UnmarshalBinary(g2Commitments[k]); err != nil {
			return fmt.Errorf("invalid G2 commitment %d: %w", k, err)
		}
		// Pairing check: e(C_k_G1, G2_gen) == e(G1_gen, C_k_G2)
		// Both sides equal e(G1_gen, G2_gen)^{a_k} iff both encode scalar a_k.
		lhs := pairingSuite.Pair(cG1, g2Gen)
		rhs := pairingSuite.Pair(g1Gen, cG2)
		if !lhs.Equal(rhs) {
			return fmt.Errorf("G1/G2 commitment %d consistency check failed: pairing mismatch", k)
		}
	}
	return nil
}

// evalCommitmentsAtIndexG2 evaluates G2 Feldman commitments at 1-based index j.
// Returns Σ_k (C_k_G2 * j^k) as a G2 point.
func evalCommitmentsAtIndexG2(commitments [][]byte, j uint32) (kyber.Point, error) {
	g2 := dkgG2Suite.G2()
	result := g2.Point().Null()
	// Use the G2 suite's scalar field (same field as G1)
	jScalar := dkgG2Suite.Scalar().SetInt64(int64(j))
	jPower := dkgG2Suite.Scalar().One()

	for _, cBytes := range commitments {
		point := g2.Point()
		if err := point.UnmarshalBinary(cBytes); err != nil {
			return nil, fmt.Errorf("invalid G2 commitment: %w", err)
		}
		term := g2.Point().Mul(jPower, point)
		result = result.Add(result, term)
		jPower = jPower.Mul(jPower, jScalar)
	}
	return result, nil
}

// computePublicShareG2FromCommitments computes the aggregate G2 public share for validator at 1-based index j.
func computePublicShareG2FromCommitments(contributions []types.DKGContribution, j uint32) ([]byte, error) {
	if j == 0 {
		return nil, fmt.Errorf("share index must be 1-based, got 0")
	}

	g2 := dkgG2Suite.G2()
	sum := g2.Point().Null()

	for _, c := range contributions {
		if len(c.FeldmanCommitmentsG2) == 0 {
			return nil, fmt.Errorf("contribution from %s missing G2 commitments", c.ValidatorAddress)
		}
		evalPoint, err := evalCommitmentsAtIndexG2(c.FeldmanCommitmentsG2, j)
		if err != nil {
			return nil, fmt.Errorf("G2 eval failed for contribution from %s: %w", c.ValidatorAddress, err)
		}
		sum = sum.Add(sum, evalPoint)
	}

	return sum.MarshalBinary()
}

// AggregateFeldmanDKGFromContributions computes TLEKeySet from all DKG contributions.
// It sums all constant-term commitments to get the master public key and evaluates
// the aggregated polynomial at each validator's index to get their public share.
func AggregateFeldmanDKGFromContributions(contributions []types.DKGContribution, dkgState types.DKGState) (*types.TLEKeySet, error) {
	masterKey, err := aggregateFeldmanMasterKey(contributions)
	if err != nil {
		return nil, fmt.Errorf("master key aggregation failed: %w", err)
	}

	// Check if all contributions include G2 commitments
	hasG2 := true
	for _, c := range contributions {
		if len(c.FeldmanCommitmentsG2) == 0 {
			hasG2 = false
			break
		}
	}

	var valShares []*types.TLEValidatorPublicShare
	for i, valAddr := range dkgState.ExpectedValidators {
		idx := uint32(i + 1)
		pubShare, err := computePublicShareFromCommitments(contributions, idx)
		if err != nil {
			return nil, fmt.Errorf("public share computation failed for %s: %w", valAddr, err)
		}

		var pubShareG2 []byte
		if hasG2 {
			pubShareG2, err = computePublicShareG2FromCommitments(contributions, idx)
			if err != nil {
				return nil, fmt.Errorf("G2 public share computation failed for %s: %w", valAddr, err)
			}
		}

		valShares = append(valShares, &types.TLEValidatorPublicShare{
			ValidatorAddress: valAddr,
			PublicShare:      pubShare,
			ShareIndex:       idx,
			PublicShareG2:    pubShareG2,
		})
	}

	return &types.TLEKeySet{
		MasterPublicKey:      masterKey,
		ThresholdNumerator:   dkgState.ThresholdNumerator,
		ThresholdDenominator: dkgState.ThresholdDenominator,
		ValidatorShares:      valShares,
	}, nil
}

package keeper

import (
	"fmt"
	stdmath "math"
	commontypes "sparkdream/x/common/types"

	"cosmossdk.io/math"

	"sparkdream/x/reveal/types"
)

// VoteKey returns the composite key for a verification vote.
func VoteKey(contributionID uint64, trancheID uint32, voter string) string {
	return fmt.Sprintf("%d/%d/%s", contributionID, trancheID, voter)
}

// TrancheKey returns the composite key for a tranche (used in secondary indexes).
func TrancheKey(contributionID uint64, trancheID uint32) string {
	return fmt.Sprintf("%d/%d", contributionID, trancheID)
}

// udreamPerExtraVote is the tranche valuation that adds one required
// verification vote: 5,000 DREAM expressed in udream (1 DREAM = 1e6 udream).
var udreamPerExtraVote = math.NewInt(5_000_000_000)

// EffectiveMinVotes calculates the scaled minimum votes for a tranche.
// effective_min_votes = max(min_verification_votes, stake_threshold / 5000 DREAM),
// with stakeThreshold in udream.
func EffectiveMinVotes(minVerificationVotes uint32, stakeThreshold math.Int) uint32 {
	scaled := stakeThreshold.Quo(udreamPerExtraVote)
	// Saturate at MaxUint32 to avoid silent narrowing when stakeThreshold is large.
	if scaled.GT(math.NewInt(int64(stdmath.MaxUint32))) {
		return stdmath.MaxUint32
	}
	scaledU32 := commontypes.Saturate[uint32](scaled.Uint64())
	if scaledU32 > minVerificationVotes {
		return scaledU32
	}
	return minVerificationVotes
}

// GetTranche safely retrieves a tranche from a contribution.
func GetTranche(contrib *types.Contribution, trancheID uint32) (*types.RevealTranche, error) {
	if int(trancheID) >= len(contrib.Tranches) {
		return nil, types.ErrTrancheNotFound
	}
	return &contrib.Tranches[trancheID], nil
}

// HasAnyTrancheReachedStatus checks if any tranche has reached the given status or beyond.
func HasAnyTrancheReachedStatus(contrib *types.Contribution, minStatus types.TrancheStatus) bool {
	for _, t := range contrib.Tranches {
		if t.Status >= minStatus {
			return true
		}
	}
	return false
}

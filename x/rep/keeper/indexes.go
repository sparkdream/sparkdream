package keeper

import (
	"context"

	"cosmossdk.io/collections"
	"sparkdream/x/rep/types"
)

// Index maintenance functions for secondary indexes.
// These must be called whenever the primary records are created, updated, or deleted.

// Initiative Index Management

// AddInitiativeToStatusIndex adds an initiative to the status index
func (k Keeper) AddInitiativeToStatusIndex(ctx context.Context, initiative types.Initiative) error {
	return k.InitiativesByStatus.Set(ctx, collections.Join(int32(initiative.Status), initiative.Id))
}

// RemoveInitiativeFromStatusIndex removes an initiative from the status index
func (k Keeper) RemoveInitiativeFromStatusIndex(ctx context.Context, status types.InitiativeStatus, id uint64) error {
	return k.InitiativesByStatus.Remove(ctx, collections.Join(int32(status), id))
}

// UpdateInitiativeStatusIndex updates the status index when initiative status changes
func (k Keeper) UpdateInitiativeStatusIndex(ctx context.Context, oldStatus, newStatus types.InitiativeStatus, id uint64) error {
	if oldStatus == newStatus {
		return nil
	}
	// Remove from old status index
	if err := k.RemoveInitiativeFromStatusIndex(ctx, oldStatus, id); err != nil {
		// Ignore not found errors (index might not exist for old initiatives)
		if !isNotFoundError(err) {
			return err
		}
	}
	// Add to new status index
	return k.InitiativesByStatus.Set(ctx, collections.Join(int32(newStatus), id))
}

// IterateInitiativesByStatus iterates over initiatives with a specific status
func (k Keeper) IterateInitiativesByStatus(ctx context.Context, status types.InitiativeStatus, fn func(id uint64) bool) error {
	rng := collections.NewPrefixedPairRange[int32, uint64](int32(status))
	return k.InitiativesByStatus.Walk(ctx, rng, func(key collections.Pair[int32, uint64]) (stop bool, err error) {
		return fn(key.K2()), nil
	})
}

// IterateInitiativesByStatuses iterates over initiatives with any of the given statuses
func (k Keeper) IterateInitiativesByStatuses(ctx context.Context, statuses []types.InitiativeStatus, fn func(id uint64, initiative types.Initiative) bool) error {
	for _, status := range statuses {
		err := k.IterateInitiativesByStatus(ctx, status, func(id uint64) bool {
			initiative, err := k.Initiative.Get(ctx, id)
			if err != nil {
				return false // Skip if not found
			}
			return fn(id, initiative)
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// Interim Index Management

// AddInterimToStatusIndex adds an interim to the status index
func (k Keeper) AddInterimToStatusIndex(ctx context.Context, interim types.Interim) error {
	return k.InterimsByStatus.Set(ctx, collections.Join(int32(interim.Status), interim.Id))
}

// RemoveInterimFromStatusIndex removes an interim from the status index
func (k Keeper) RemoveInterimFromStatusIndex(ctx context.Context, status types.InterimStatus, id uint64) error {
	return k.InterimsByStatus.Remove(ctx, collections.Join(int32(status), id))
}

// UpdateInterimStatusIndex updates the status index when interim status changes
func (k Keeper) UpdateInterimStatusIndex(ctx context.Context, oldStatus, newStatus types.InterimStatus, id uint64) error {
	if oldStatus == newStatus {
		return nil
	}
	if err := k.RemoveInterimFromStatusIndex(ctx, oldStatus, id); err != nil {
		if !isNotFoundError(err) {
			return err
		}
	}
	return k.InterimsByStatus.Set(ctx, collections.Join(int32(newStatus), id))
}

// IterateInterimsByStatus iterates over interims with a specific status
func (k Keeper) IterateInterimsByStatus(ctx context.Context, status types.InterimStatus, fn func(id uint64) bool) error {
	rng := collections.NewPrefixedPairRange[int32, uint64](int32(status))
	return k.InterimsByStatus.Walk(ctx, rng, func(key collections.Pair[int32, uint64]) (stop bool, err error) {
		return fn(key.K2()), nil
	})
}

// IterateInterimsByStatuses iterates over interims with any of the given statuses
func (k Keeper) IterateInterimsByStatuses(ctx context.Context, statuses []types.InterimStatus, fn func(id uint64, interim types.Interim) bool) error {
	for _, status := range statuses {
		err := k.IterateInterimsByStatus(ctx, status, func(id uint64) bool {
			interim, err := k.Interim.Get(ctx, id)
			if err != nil {
				return false
			}
			return fn(id, interim)
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// JuryReview Index Management

// AddJuryReviewToVerdictIndex adds a jury review to the verdict index
func (k Keeper) AddJuryReviewToVerdictIndex(ctx context.Context, review types.JuryReview) error {
	return k.JuryReviewsByVerdict.Set(ctx, collections.Join(int32(review.Verdict), review.Id))
}

// RemoveJuryReviewFromVerdictIndex removes a jury review from the verdict index
func (k Keeper) RemoveJuryReviewFromVerdictIndex(ctx context.Context, verdict types.Verdict, id uint64) error {
	return k.JuryReviewsByVerdict.Remove(ctx, collections.Join(int32(verdict), id))
}

// UpdateJuryReviewVerdictIndex updates the verdict index when verdict changes
func (k Keeper) UpdateJuryReviewVerdictIndex(ctx context.Context, oldVerdict, newVerdict types.Verdict, id uint64) error {
	if oldVerdict == newVerdict {
		return nil
	}
	if err := k.RemoveJuryReviewFromVerdictIndex(ctx, oldVerdict, id); err != nil {
		if !isNotFoundError(err) {
			return err
		}
	}
	return k.JuryReviewsByVerdict.Set(ctx, collections.Join(int32(newVerdict), id))
}

// IterateJuryReviewsByVerdict iterates over jury reviews with a specific verdict
func (k Keeper) IterateJuryReviewsByVerdict(ctx context.Context, verdict types.Verdict, fn func(id uint64, review types.JuryReview) bool) error {
	rng := collections.NewPrefixedPairRange[int32, uint64](int32(verdict))
	return k.JuryReviewsByVerdict.Walk(ctx, rng, func(key collections.Pair[int32, uint64]) (stop bool, err error) {
		review, err := k.JuryReview.Get(ctx, key.K2())
		if err != nil {
			return false, nil // Skip if not found
		}
		return fn(key.K2(), review), nil
	})
}

// Stake Index Management

// AddStakeToTargetIndex adds a stake to the target index
func (k Keeper) AddStakeToTargetIndex(ctx context.Context, stake types.Stake) error {
	return k.StakesByTarget.Set(ctx, collections.Join3(int32(stake.TargetType), stake.TargetId, stake.Id))
}

// RemoveStakeFromTargetIndex removes a stake from the target index
func (k Keeper) RemoveStakeFromTargetIndex(ctx context.Context, stake types.Stake) error {
	return k.StakesByTarget.Remove(ctx, collections.Join3(int32(stake.TargetType), stake.TargetId, stake.Id))
}

// GetStakesByTarget returns all stakes for a specific target using the index
func (k Keeper) GetStakesByTarget(ctx context.Context, targetType types.StakeTargetType, targetID uint64) ([]types.Stake, error) {
	var stakes []types.Stake

	// Use prefix range to get all stakes for this target
	rng := collections.NewSuperPrefixedTripleRange[int32, uint64, uint64](int32(targetType), targetID)
	err := k.StakesByTarget.Walk(ctx, rng, func(key collections.Triple[int32, uint64, uint64]) (stop bool, err error) {
		stakeID := key.K3()
		stake, err := k.Stake.Get(ctx, stakeID)
		if err != nil {
			return false, nil // Skip if not found (stale index entry)
		}
		stakes = append(stakes, stake)
		return false, nil
	})
	if err != nil {
		return nil, err
	}

	return stakes, nil
}

// Challenge Index Management

// AddChallengeToStatusIndex adds a challenge to the status index
func (k Keeper) AddChallengeToStatusIndex(ctx context.Context, challenge types.Challenge) error {
	return k.ChallengesByStatus.Set(ctx, collections.Join(int32(challenge.Status), challenge.Id))
}

// RemoveChallengeFromStatusIndex removes a challenge from the status index
func (k Keeper) RemoveChallengeFromStatusIndex(ctx context.Context, status types.ChallengeStatus, id uint64) error {
	return k.ChallengesByStatus.Remove(ctx, collections.Join(int32(status), id))
}

// UpdateChallengeStatusIndex updates the status index when challenge status changes
func (k Keeper) UpdateChallengeStatusIndex(ctx context.Context, oldStatus, newStatus types.ChallengeStatus, id uint64) error {
	if oldStatus == newStatus {
		return nil
	}
	if err := k.RemoveChallengeFromStatusIndex(ctx, oldStatus, id); err != nil {
		if !isNotFoundError(err) {
			return err
		}
	}
	return k.ChallengesByStatus.Set(ctx, collections.Join(int32(newStatus), id))
}

// IterateChallengesByStatus iterates over challenges with a specific status
func (k Keeper) IterateChallengesByStatus(ctx context.Context, status types.ChallengeStatus, fn func(id uint64, challenge types.Challenge) bool) error {
	rng := collections.NewPrefixedPairRange[int32, uint64](int32(status))
	return k.ChallengesByStatus.Walk(ctx, rng, func(key collections.Pair[int32, uint64]) (stop bool, err error) {
		challenge, err := k.Challenge.Get(ctx, key.K2())
		if err != nil {
			return false, nil // Skip if not found
		}
		return fn(key.K2(), challenge), nil
	})
}

// Helper to check if error is "not found"
func isNotFoundError(err error) bool {
	return err != nil && err.Error() == "not found"
}

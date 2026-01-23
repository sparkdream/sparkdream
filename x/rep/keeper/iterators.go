package keeper

import (
	"context"
	"sparkdream/x/rep/types"
)

// IterateActiveInitiatives iterates over all initiatives that are in a state where conviction should be updated.
// Uses the status index for O(active) instead of O(all) complexity.
func (k Keeper) IterateActiveInitiatives(ctx context.Context, fn func(index int64, initiative types.Initiative) (stop bool)) {
	activeStatuses := []types.InitiativeStatus{
		types.InitiativeStatus_INITIATIVE_STATUS_OPEN,
		types.InitiativeStatus_INITIATIVE_STATUS_ASSIGNED,
		types.InitiativeStatus_INITIATIVE_STATUS_SUBMITTED,
		types.InitiativeStatus_INITIATIVE_STATUS_IN_REVIEW,
		types.InitiativeStatus_INITIATIVE_STATUS_CHALLENGED,
	}

	_ = k.IterateInitiativesByStatuses(ctx, activeStatuses, func(id uint64, initiative types.Initiative) bool {
		return fn(int64(id), initiative)
	})
}

// IterateSubmittedInitiatives iterates over initiatives that have been submitted but not yet in review/challenge period.
// Uses the status index for O(submitted) instead of O(all) complexity.
func (k Keeper) IterateSubmittedInitiatives(ctx context.Context, fn func(index int64, initiative types.Initiative) (stop bool)) {
	_ = k.IterateInitiativesByStatuses(ctx, []types.InitiativeStatus{
		types.InitiativeStatus_INITIATIVE_STATUS_SUBMITTED,
	}, func(id uint64, initiative types.Initiative) bool {
		return fn(int64(id), initiative)
	})
}

// IteratePendingCompletionInitiatives iterates over initiatives in the review/challenge period.
// Uses the status index for O(in_review) instead of O(all) complexity.
func (k Keeper) IteratePendingCompletionInitiatives(ctx context.Context, fn func(index int64, initiative types.Initiative) (stop bool)) {
	_ = k.IterateInitiativesByStatuses(ctx, []types.InitiativeStatus{
		types.InitiativeStatus_INITIATIVE_STATUS_IN_REVIEW,
	}, func(id uint64, initiative types.Initiative) bool {
		return fn(int64(id), initiative)
	})
}

// IterateActiveJuryReviews iterates over active jury reviews.
// Uses the verdict index for O(pending) instead of O(all) complexity.
func (k Keeper) IterateActiveJuryReviews(ctx context.Context, fn func(index int64, review types.JuryReview) (stop bool)) {
	_ = k.IterateJuryReviewsByVerdict(ctx, types.Verdict_VERDICT_PENDING, func(id uint64, review types.JuryReview) bool {
		return fn(int64(id), review)
	})
}

// IteratePendingInterims iterates over pending interims (assigned but not completed/expired).
// Uses the status index for O(pending+in_progress) instead of O(all) complexity.
func (k Keeper) IteratePendingInterims(ctx context.Context, fn func(index int64, interim types.Interim) (stop bool)) {
	_ = k.IterateInterimsByStatuses(ctx, []types.InterimStatus{
		types.InterimStatus_INTERIM_STATUS_PENDING,
		types.InterimStatus_INTERIM_STATUS_IN_PROGRESS,
	}, func(id uint64, interim types.Interim) bool {
		return fn(int64(id), interim)
	})
}

// IterateActiveChallenges iterates over active challenges (awaiting response).
// Uses the status index for O(active) instead of O(all) complexity.
func (k Keeper) IterateActiveChallenges(ctx context.Context, fn func(index int64, challenge types.Challenge) (stop bool)) {
	_ = k.IterateChallengesByStatus(ctx, types.ChallengeStatus_CHALLENGE_STATUS_ACTIVE, func(id uint64, challenge types.Challenge) bool {
		return fn(int64(id), challenge)
	})
}

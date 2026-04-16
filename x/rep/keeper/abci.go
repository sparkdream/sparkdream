package keeper

import (
	"context"
	"sparkdream/x/rep/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// EndBlocker implements the end blocker logic
func (k Keeper) EndBlocker(ctx context.Context) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// 1. Update conviction for all active initiative stakes
	k.IterateActiveInitiatives(ctx, func(index int64, initiative types.Initiative) bool {
		// We update conviction for each active initiative
		// This recalculates based on time elapsed for all stakes
		if err := k.UpdateInitiativeConviction(ctx, initiative.Id); err != nil {
			sdkCtx.Logger().Error("failed to update initiative conviction", "initiative_id", initiative.Id, "error", err)
		}
		return false
	})

	// 2. Check initiative completion thresholds
	k.IterateSubmittedInitiatives(ctx, func(index int64, initiative types.Initiative) bool {
		canComplete, err := k.CanCompleteInitiative(ctx, initiative.Id)
		if err != nil {
			sdkCtx.Logger().Error("failed to check initiative completion", "initiative_id", initiative.Id, "error", err)
		} else if canComplete {
			if err := k.TransitionToChallengePeriod(ctx, initiative.Id); err != nil {
				sdkCtx.Logger().Error("failed to transition initiative to challenge period", "initiative_id", initiative.Id, "error", err)
			}
		}
		return false
	})

	// 3. Finalize unchallenged initiatives
	k.IteratePendingCompletionInitiatives(ctx, func(index int64, initiative types.Initiative) bool {
		if sdkCtx.BlockHeight() >= initiative.ChallengePeriodEnd {
			if err := k.CompleteInitiative(ctx, initiative.Id); err != nil {
				sdkCtx.Logger().Error("failed to complete initiative", "initiative_id", initiative.Id, "error", err)
			}
		}
		return false
	})

	// 4. DREAM decay is applied lazily via GetMember/GetBalance
	// No bulk decay needed - decay is calculated on-demand when members are accessed
	// This scales O(1) per block instead of O(n) where n = member count

	// 5. Process expired challenge responses
	// If assignee doesn't respond within the deadline, challenge is auto-upheld
	k.IterateActiveChallenges(ctx, func(index int64, challenge types.Challenge) bool {
		if challenge.ResponseDeadline > 0 && sdkCtx.BlockHeight() >= challenge.ResponseDeadline {
			// Auto-uphold the challenge - assignee failed to respond
			if err := k.UpholdChallenge(ctx, challenge.Id); err != nil {
				sdkCtx.Logger().Error("failed to uphold challenge", "challenge_id", challenge.Id, "error", err)
			}
		}
		return false
	})

	// 5b. Process expired content challenge responses
	// If author doesn't respond within the deadline, challenge is auto-upheld
	k.IterateActiveContentChallenges(ctx, func(index int64, cc types.ContentChallenge) bool {
		if cc.ResponseDeadline > 0 && sdkCtx.BlockHeight() >= cc.ResponseDeadline {
			if err := k.UpholdContentChallenge(ctx, cc.Id); err != nil {
				sdkCtx.Logger().Error("failed to uphold content challenge", "content_challenge_id", cc.Id, "error", err)
			}
		}
		return false
	})

	// 6. Process jury review deadlines
	k.IterateActiveJuryReviews(ctx, func(index int64, review types.JuryReview) bool {
		if sdkCtx.BlockHeight() >= review.Deadline {
			if err := k.TallyJuryVotes(ctx, review.Id); err != nil {
				sdkCtx.Logger().Error("failed to tally jury votes", "review_id", review.Id, "error", err)
			}
		}
		return false
	})

	// 7. Process assigned initiative deadlines (interims)
	k.IteratePendingInterims(ctx, func(index int64, interim types.Interim) bool {
		if sdkCtx.BlockHeight() >= interim.Deadline {
			if err := k.ExpireInterim(ctx, interim.Id); err != nil {
				sdkCtx.Logger().Error("failed to expire interim", "interim_id", interim.Id, "error", err)
			}
		}
		return false
	})

	// 8. Distribute staking rewards from seasonal pool
	if err := k.DistributeEpochStakingRewards(ctx); err != nil {
		return err
	}

	// 9. Treasury overflow check (enforced each epoch boundary)
	if err := k.EnforceTreasuryBalance(ctx); err != nil {
		return err
	}

	// 10. Trust levels are updated lazily at trigger points:
	//    - When a member completes an interim (reputation gained)
	//    - When reputation is granted/reduced
	//    - When a new season starts
	// No bulk update needed - this scales O(1) per block instead of O(n*m)
	// where n = member count and m = interim count

	// 11. Process invitation accountability
	if err := k.ProcessExpiredAccountability(ctx); err != nil {
		return err
	}

	// 12. Rebuild member trust tree if dirty (for anonymous posting ZK proofs)
	if err := k.MaybeRebuildTrustTree(ctx); err != nil {
		return err
	}

	// 13. Invitation credits are reset lazily via EnsureInvitationCreditsReset
	// When a member tries to invite, we check if the current season > their last reset season
	// If so, we reset their credits to their trust-level max
	// This scales O(1) per block instead of O(n) where n = member count

	return nil
}

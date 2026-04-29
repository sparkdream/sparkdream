package keeper

import (
	"context"
	"fmt"
	"sparkdream/x/rep/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
)

const maxTagExpirations = 50

// EndBlocker implements the end blocker logic
func (k Keeper) EndBlocker(ctx context.Context) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// 0. Apply DREAM decay to every member once per epoch. Running this first
	// guarantees subsequent EndBlocker steps (staking rewards, conviction, etc.)
	// and all reads during the epoch see a consistent post-decay balance,
	// eliminating the lazy-decay view inconsistency.
	if err := k.MaybeApplyBulkDecay(ctx); err != nil {
		sdkCtx.Logger().Error("failed to apply bulk decay", "error", err)
	}

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

	// 4. DREAM decay: bulk pass in step 0 applies decay once per epoch for every
	// member so same-epoch reads stay consistent. The lazy ApplyPendingDecay on
	// write paths remains as a safety net (becomes a no-op once bulk pass runs).

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

	// 14. Expire unused tags
	if err := k.ExpireTags(ctx, sdkCtx.BlockTime().Unix()); err != nil {
		sdkCtx.Logger().Error("error expiring tags", "error", err)
	}

	// 15a. Distribute sentinel reward pool to eligible sentinels on the
	// sentinel-reward epoch boundary (Stage D). Must run BEFORE the overflow
	// burn so distribution drains first and the burn only targets residual.
	if err := k.DistributeSentinelRewards(ctx); err != nil {
		sdkCtx.Logger().Error("error distributing sentinel rewards", "error", err)
	}

	// 15b. Burn sentinel reward pool overflow (Stage A).
	if err := k.BurnSentinelRewardPoolOverflow(ctx); err != nil {
		sdkCtx.Logger().Error("error burning sentinel reward pool overflow", "error", err)
	}

	// 16. Time out expired gov action appeals (half refund / half burn).
	if err := k.TimeoutExpiredAppeals(ctx); err != nil {
		sdkCtx.Logger().Error("error timing out expired gov action appeals", "error", err)
	}

	return nil
}

// BurnSentinelRewardPoolOverflow checks whether the sentinel SPARK reward pool
// (rep module account's uspark balance) exceeds `MaxSentinelRewardPool`. If it
// does, a fraction `SentinelRewardPoolOverflowBurnRatio` of the overflow is
// burned from the rep module account. The remaining overflow stays in the pool
// to be distributed on the next epoch boundary (Stage D).
//
// This is a no-op when the pool is at or below the cap.
func (k Keeper) BurnSentinelRewardPoolOverflow(ctx context.Context) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	params, err := k.Params.Get(ctx)
	if err != nil {
		return fmt.Errorf("load params: %w", err)
	}

	maxPool := params.MaxSentinelRewardPool
	burnRatio := params.SentinelRewardPoolOverflowBurnRatio

	current := k.GetSentinelRewardPool(ctx)
	if !current.GT(maxPool) {
		return nil
	}

	overflow := current.Sub(maxPool)
	burnAmount := burnRatio.MulInt(overflow).TruncateInt()
	if !burnAmount.IsPositive() {
		return nil
	}

	// BurnCoins requires a registered module account with Burner permission, so
	// move the overflow from the sentinel sub-address to the rep module account
	// (which holds Burner) and then burn from there. The two ops are atomic
	// inside this BeginBlocker call, so no other path observes the intermediate
	// balance on the rep module account.
	coins := sdk.NewCoins(sdk.NewCoin(types.RewardDenom, burnAmount))
	if err := k.bankKeeper.SendCoins(ctx, SentinelRewardPoolAddress(), authtypes.NewModuleAddress(types.ModuleName), coins); err != nil {
		return fmt.Errorf("move sentinel overflow to module account: %w", err)
	}
	if err := k.bankKeeper.BurnCoins(ctx, types.ModuleName, coins); err != nil {
		return fmt.Errorf("burn sentinel reward pool overflow: %w", err)
	}

	sdkCtx.EventManager().EmitEvent(sdk.NewEvent("sentinel_reward_pool_overflow",
		sdk.NewAttribute("burned", burnAmount.String()),
		sdk.NewAttribute("overflow", overflow.String()),
		sdk.NewAttribute("pool_before", current.String()),
		sdk.NewAttribute("max_pool", maxPool.String()),
		sdk.NewAttribute("burn_ratio", burnRatio.String()),
	))

	return nil
}

// ExpireTags removes tags whose expiration_index has passed and that are not
// reserved. Usage-active tags update their expiration on IncrementTagUsage.
func (k Keeper) ExpireTags(ctx context.Context, now int64) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Collect candidates during iteration, remove after the iterator closes
	// to avoid mutation-during-iteration undefined behavior.
	type expiredTag struct {
		name            string
		expirationIndex int64
	}
	var toRemove []expiredTag
	err := k.Tag.Walk(ctx, nil, func(name string, tag types.Tag) (bool, error) {
		if len(toRemove) >= maxTagExpirations {
			return true, nil
		}
		if tag.ExpirationIndex <= 0 {
			return false, nil
		}
		if tag.ExpirationIndex > now {
			return false, nil
		}
		if reserved, rErr := k.ReservedTag.Has(ctx, name); rErr == nil && reserved {
			return false, nil
		}
		toRemove = append(toRemove, expiredTag{name: name, expirationIndex: tag.ExpirationIndex})
		return false, nil
	})
	if err != nil {
		return nil
	}

	expired := 0
	for _, t := range toRemove {
		if rmErr := k.Tag.Remove(ctx, t.name); rmErr != nil {
			sdkCtx.Logger().Error("failed to remove expired tag", "tag", t.name, "error", rmErr)
			continue
		}
		if k.late.forumKeeper != nil {
			// Best-effort cleanup of stale references; non-fatal.
			_ = k.late.forumKeeper.PruneTagReferences(ctx, t.name)
		}
		sdkCtx.EventManager().EmitEvent(sdk.NewEvent("tag_expired",
			sdk.NewAttribute("tag_name", t.name),
			sdk.NewAttribute("expiration_index", fmt.Sprintf("%d", t.expirationIndex)),
		))
		expired++
	}
	if expired > 0 {
		sdkCtx.Logger().Info("expired tags", "count", expired)
	}
	return nil
}

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

	// 1. Recompute conviction for initiatives whose refresh is due, under a
	// per-block work budget. This replaces a sweep over every stake of every
	// active initiative on every block, whose cost scaled with a stake count any
	// member could inflate for a refundable amount of DREAM. See
	// conviction_queue.go for the scheduling model.
	if err := k.DrainConvictionQueue(ctx); err != nil {
		sdkCtx.Logger().Error("failed to drain conviction queue", "error", err)
	}

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
			// Skip payout for initiatives whose parent project was cancelled
			// after they entered review — CompleteInitiative would reject them
			// anyway; skipping keeps that off the per-block error log. The
			// assignee reclaims their bond via AbandonInitiative.
			if project, perr := k.GetProject(ctx, initiative.ProjectId); perr == nil &&
				project.Status == types.ProjectStatus_PROJECT_STATUS_CANCELLED {
				return false
			}
			// CompleteInitiative mints to the completer, the treasury, and every
			// staker before it deletes the stake records, and it is not
			// idempotent. EndBlocker writes straight to the deliver state, so a
			// mid-function error here would persist the mints already made and
			// leave the initiative to be retried next block — paying them
			// twice. Run it in a child cache context and only commit on
			// success. (The msg-server path already gets this from the SDK.)
			cacheCtx, writeCache := sdkCtx.CacheContext()
			if err := k.CompleteInitiative(cacheCtx, initiative.Id); err != nil {
				sdkCtx.Logger().Error("failed to complete initiative", "initiative_id", initiative.Id, "error", err)
			} else {
				writeCache()
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

	// 6. Resolve challenge / content-challenge jury reviews whose (block-height)
	// deadline passed without reaching a verdict by votes. Appeals are NOT
	// handled here (timestamp deadline) — they resolve via the vote-triggered
	// path and, at their deadline, via TimeoutExpiredAppeals below.
	if err := k.ResolveExpiredChallengeJuryReviews(ctx); err != nil {
		sdkCtx.Logger().Error("failed to resolve expired challenge jury reviews", "error", err)
	}

	// 6b. Vacate and redraw jury seats nobody answered. Runs before the tally
	// sweep above has any effect on a given review because the acceptance
	// window is far shorter than the vote deadline — the point is to replace a
	// silent juror while the review still has time to run, rather than
	// discovering the absence when the deadline forces an INCONCLUSIVE tally.
	if err := k.SweepUnansweredJurySeats(ctx); err != nil {
		sdkCtx.Logger().Error("failed to sweep unanswered jury seats", "error", err)
	}

	// 6c. Escalate review rounds nobody finished. With the staker veto retired,
	// reviewers are the quality gate — so if nobody reviews, nothing completes.
	// This is the liveness guarantee: past the deadline the round goes to the
	// Operations Committee, and committee silence resolves to PASSED rather than
	// leaving the initiative wedged.
	if err := k.SweepReviewDeadlines(ctx); err != nil {
		sdkCtx.Logger().Error("failed to sweep review deadlines", "error", err)
	}

	// 7. Process assigned initiative deadlines (interims).
	//
	// Collect first, then expire. ExpireInterim moves the interim out of
	// PENDING/IN_PROGRESS, which is the very index IteratePendingInterims is
	// walking, and it now also resolves an adjudication's challenge — so
	// mutating mid-walk is doubly unsafe. Same shape as
	// ResolveExpiredChallengeJuryReviews above.
	var dueInterims []uint64
	k.IteratePendingInterims(ctx, func(index int64, interim types.Interim) bool {
		if sdkCtx.BlockHeight() >= interim.Deadline {
			dueInterims = append(dueInterims, interim.Id)
		}
		return false
	})
	for _, id := range dueInterims {
		if err := k.ExpireInterim(ctx, id); err != nil {
			sdkCtx.Logger().Error("failed to expire interim", "interim_id", id, "error", err)
		}
	}

	// 7b. Expire stale PROPOSED projects that no committee has approved within
	// their expiry window. Collect first, mutate after the iterator closes so
	// we don't mutate the by-status index mid-walk.
	var expiredProjects []uint64
	if err := k.IterateProjectsByStatus(ctx, types.ProjectStatus_PROJECT_STATUS_PROPOSED, func(id uint64, project types.Project) bool {
		if project.ExpiryBlockHeight > 0 && sdkCtx.BlockHeight() >= project.ExpiryBlockHeight {
			expiredProjects = append(expiredProjects, id)
		}
		return false
	}); err != nil {
		sdkCtx.Logger().Error("failed to walk PROPOSED projects for expiry", "error", err)
	}
	for _, id := range expiredProjects {
		if err := k.ExpireProject(ctx, id); err != nil {
			sdkCtx.Logger().Error("failed to expire project", "project_id", id, "error", err)
		}
	}

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

	// 17. Mature any in-flight bonded-role unbonds whose cooldown has elapsed:
	// unlocks the pending DREAM, flips status to DEMOTED, and starts
	// demotion_cooldown gating re-bonding. Slashes during the cooldown have
	// already reduced pending_unbond_amount in SlashBond, so the holder gets
	// back whatever's left.
	if err := k.MatureUnbonds(ctx); err != nil {
		sdkCtx.Logger().Error("error maturing bonded-role unbonds", "error", err)
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
	coins := sdk.NewCoins(sdk.NewCoin(k.BondDenom(ctx), burnAmount))
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

// ExpireTags removes tags that have fallen idle past DefaultTagExpiration
// and that are not reserved. The GC trigger is `last_used_at +
// DefaultTagExpiration <= now`; IncrementTagUsage refreshes last_used_at,
// so actively referenced tags roll their deadline forward and survive,
// while misspellings and stale tags hit their deadline and get reclaimed.
//
// Tags with last_used_at <= 0 are treated as permanent and skipped (used
// by genesis-seeded sentinel tags that should never GC).
func (k Keeper) ExpireTags(ctx context.Context, now int64) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Collect candidates during iteration, remove after the iterator closes
	// to avoid mutation-during-iteration undefined behavior.
	type expiredTag struct {
		name       string
		lastUsedAt int64
		expiresAt  int64
	}
	var toRemove []expiredTag
	err := k.Tag.Walk(ctx, nil, func(name string, tag types.Tag) (bool, error) {
		if len(toRemove) >= maxTagExpirations {
			return true, nil
		}
		if tag.LastUsedAt <= 0 {
			return false, nil
		}
		expiresAt := tag.LastUsedAt + types.DefaultTagExpiration
		if expiresAt > now {
			return false, nil
		}
		if reserved, rErr := k.ReservedTag.Has(ctx, name); rErr == nil && reserved {
			return false, nil
		}
		toRemove = append(toRemove, expiredTag{
			name: name, lastUsedAt: tag.LastUsedAt, expiresAt: expiresAt,
		})
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
			sdk.NewAttribute("last_used_at", fmt.Sprintf("%d", t.lastUsedAt)),
			sdk.NewAttribute("expired_at", fmt.Sprintf("%d", t.expiresAt)),
		))
		expired++
	}
	if expired > 0 {
		sdkCtx.Logger().Info("expired tags", "count", expired)
	}
	return nil
}

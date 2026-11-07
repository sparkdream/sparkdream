package keeper

import (
	"context"
	"fmt"
	"sparkdream/x/rep/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

const maxTagExpirations = 50

// BeginBlocker tops up the bonded-role reward pools from the community pool
// and draws the jury seats queued by earlier blocks.
//
// Funding runs in BeginBlock rather than EndBlock so a pool is funded before
// the same block's reward distribution reads its balance — funding that
// landed one block late would silently shrink the epoch payout it was meant
// to cover.
//
// The draw runs in BeginBlock so it sees only state fixed before its seed
// was knowable. x/shield's PreBlocker rebuilds a beacon value from the
// previous block's vote extensions, which anyone can read before this
// block's transactions are chosen; drawn in the EndBlocker, a proposer who
// computed the outcome could include transactions that change the juror
// pool or its weights and move who is seated. Here the pool is the state at
// the end of the previous block, settled before any validator signed the
// shares.
//
// A failure must never halt the chain: both steps swallow their own
// recoverable errors and anything that escapes is logged, not returned.
func (k Keeper) BeginBlocker(ctx context.Context) error {
	if err := k.FundRoleRewardPools(ctx); err != nil {
		sdk.UnwrapSDKContext(ctx).Logger().Error("failed to fund role reward pools", "error", err)
	}
	if err := k.DrawPendingJuries(ctx); err != nil {
		sdk.UnwrapSDKContext(ctx).Logger().Error("failed to draw pending jury seats", "error", err)
	}
	return nil
}

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

	// 2. Check initiative completion thresholds: a capped round-robin over
	// SUBMITTED initiatives (REP-E-10); see submitted_check.go.
	k.CheckSubmittedInitiatives(ctx)

	// 3. Finalize unchallenged initiatives whose challenge period ended.
	// Reads the completion due queue up to this height, capped per block
	// (REP-E-10); see due_queue.go.
	k.SweepDueInitiativeCompletions(ctx)

	// 4. DREAM decay: bulk pass in step 0 applies decay once per epoch for every
	// member so same-epoch reads stay consistent. The lazy ApplyPendingDecay on
	// write paths remains as a safety net (becomes a no-op once bulk pass runs).

	// 5. Process expired challenge responses: an ACTIVE challenge whose
	// assignee did not respond by the deadline is auto-upheld. Due queue,
	// capped per block (REP-E-10).
	k.SweepDueChallengeResponses(ctx)

	// 5b. Process expired content challenge responses: an author who did not
	// respond by the deadline has the challenge auto-upheld. Due queue,
	// capped per block (REP-E-10).
	k.SweepDueContentChallengeResponses(ctx)

	// 6. Resolve challenge / content-challenge jury reviews whose (block-height)
	// deadline passed without reaching a verdict by votes. Appeals are NOT
	// handled here (timestamp deadline) — they resolve via the vote-triggered
	// path and, at their deadline, via TimeoutExpiredAppeals below.
	if err := k.ResolveExpiredChallengeJuryReviews(ctx); err != nil {
		sdkCtx.Logger().Error("failed to resolve expired challenge jury reviews", "error", err)
	}

	// 6a. Content-challenge jury reviews are excluded from the shared PENDING
	// verdict index that step 6 sweeps (initiative-challenge semantics resolved
	// them out from under their callers), so they get their own timeout path.
	// Without it a content jury that never voted locked the challenger's stake,
	// the author's bond and the target's challenge slot permanently.
	if err := k.SweepExpiredContentJuryReviews(ctx); err != nil {
		sdkCtx.Logger().Error("failed to sweep expired content jury reviews", "error", err)
	}

	// 6b. Vacate and redraw jury seats nobody answered. Runs before the tally
	// sweep above has any effect on a given review because the acceptance
	// window is far shorter than the vote deadline — the point is to replace a
	// silent juror while the review still has time to run, rather than
	// discovering the absence when the deadline forces an INCONCLUSIVE tally.
	if err := k.SweepUnansweredJurySeats(ctx); err != nil {
		sdkCtx.Logger().Error("failed to sweep unanswered jury seats", "error", err)
	}

	// 6b'. Seats queued this block (new reviews, declined seats, the seats
	// the sweep above just vacated) are drawn in the next BeginBlocker, never
	// in the transaction that asked for them (REP-R-102); see BeginBlocker.

	// 6c. Escalate review rounds nobody finished. With the staker veto retired,
	// reviewers are the quality gate — so if nobody reviews, nothing completes.
	// This is the liveness guarantee: past the deadline the round goes to the
	// Operations Committee, and committee silence rejects the round rather than
	// leaving the initiative wedged. The assignee resubmits for a new round
	// (bounded by max_review_rounds); when the rounds run out the initiative
	// closes cleanly (budget returned, bond released, nothing minted). Silence
	// never passes or mints.
	if err := k.SweepReviewDeadlines(ctx); err != nil {
		sdkCtx.Logger().Error("failed to sweep review deadlines", "error", err)
	}

	// 7. Expire interims whose deadline passed. Due queue, capped per block
	// (REP-E-201): the walk over every live interim this used to be grew
	// with each budget-backed project proposal, which opens one committee
	// review interim; see due_queue.go.
	k.SweepDueInterimDeadlines(ctx)

	// 7b. Expire stale PROPOSED projects that no committee approved within
	// their expiry window. Due queue, capped per block (REP-E-201).
	k.SweepDueProjectExpiries(ctx)

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

	// 15c. Same pair for the initiative-reviewer SPARK pool, on its own cadence
	// and against its own cap. Distribution runs before the burn for the same
	// reason: drain to the people who earned it first, burn only the residual.
	if err := k.DistributeReviewerRewards(ctx); err != nil {
		sdkCtx.Logger().Error("error distributing reviewer rewards", "error", err)
	}
	if err := k.BurnReviewerRewardPoolOverflow(ctx); err != nil {
		sdkCtx.Logger().Error("error burning reviewer reward pool overflow", "error", err)
	}

	// 15d. Same pair for the collect-curator SPARK pool.
	if err := k.DistributeCuratorRewards(ctx); err != nil {
		sdkCtx.Logger().Error("error distributing curator rewards", "error", err)
	}
	if err := k.BurnCuratorRewardPoolOverflow(ctx); err != nil {
		sdkCtx.Logger().Error("error burning curator reward pool overflow", "error", err)
	}

	// 15e. Same pair for the federation-verifier SPARK pool. This one also
	// mints the verifier DREAM stipend, so it is the single distribution for
	// the role -- x/federation reports verifications and verdicts here and
	// pays nothing itself. See verifier_reward_distribution.go for why the
	// role's pay lives in x/rep rather than in the module that owns the
	// verification surface.
	if err := k.DistributeVerifierRewards(ctx); err != nil {
		sdkCtx.Logger().Error("error distributing verifier rewards", "error", err)
	}
	if err := k.BurnVerifierRewardPoolOverflow(ctx); err != nil {
		sdkCtx.Logger().Error("error burning verifier reward pool overflow", "error", err)
	}

	// 16. Time out expired gov action appeals (half refund / half burn).
	if err := k.TimeoutExpiredAppeals(ctx); err != nil {
		sdkCtx.Logger().Error("error timing out expired gov action appeals", "error", err)
	}
	// 16a. Drop member-action restoration records whose filing window closed
	// with no appeal (capped per block).
	k.PruneMemberActionRestorations(ctx)

	// 17. Mature any in-flight bonded-role unbonds whose cooldown has elapsed:
	// unlocks the pending DREAM, flips status to DEMOTED, and starts
	// demotion_cooldown gating re-bonding. Slashes during the cooldown have
	// already reduced pending_unbond_amount in SlashBond, so the holder gets
	// back whatever's left.
	if err := k.MatureUnbonds(ctx); err != nil {
		sdkCtx.Logger().Error("error maturing bonded-role unbonds", "error", err)
	}

	// 18. Member key migration: finalize due migrations, expire unaccepted
	// ones and stale recovery seals, tally overdue objection juries and
	// activate pending ZK keys. Every pass is capped and contained; see
	// migration_endblock.go. Runs before the trust tree rebuild would be
	// ideal, but step 12 already ran: a finalized migration marks the new
	// address dirty and the next block's rebuild picks it up.
	k.ProcessMigrations(ctx)

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
	// Routed through SendCoinsFromAccountToModule rather than a plain SendCoins
	// to the raw module address: a plain send auto-creates a BaseAccount at
	// that address, and the BurnCoins below then resolves it as a module
	// account and PANICS ("account is not a module account"). x/forum used to
	// instantiate rep's module account properly as a side effect of routing
	// its spam tax through it, which masked this at every burn site here.
	if err := k.bankKeeper.SendCoinsFromAccountToModule(ctx, SentinelRewardPoolAddress(), types.ModuleName, coins); err != nil {
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
		// A tag with an open report waits for its resolution (REP-E-11):
		// collecting it made ResolveTagReport fail on the missing tag and
		// stranded the reporters' bonds.
		if reported, rErr := k.TagReport.Has(ctx, name); rErr == nil && reported {
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
		if rmErr := k.RemoveTag(ctx, t.name); rmErr != nil {
			sdkCtx.Logger().Error("failed to remove expired tag", "tag", t.name, "error", rmErr)
			continue
		}
		if k.late.forumKeeper != nil {
			// Best-effort cleanup of stale references; non-fatal.
			if err := k.late.forumKeeper.PruneTagReferences(ctx, t.name); err != nil {
				sdk.UnwrapSDKContext(ctx).Logger().Error("ExpireTags: late.forumKeeper.PruneTagReferences failed", "err", err)
			}
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

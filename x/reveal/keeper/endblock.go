package keeper

import (
	"context"
	"errors"
	"fmt"
	stdmath "math"
	commontypes "sparkdream/x/common/types"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	reptypes "sparkdream/x/rep/types"
	"sparkdream/x/reveal/types"
)

// maxDeadlineContributionsPerBlock bounds how many due contributions the
// EndBlocker processes per block; the rest stay in the deadline index and
// are picked up on the following blocks.
const maxDeadlineContributionsPerBlock = 100

// ProcessDeadlines runs every block and enforces all deadline-based transitions.
// It walks only the contributions whose earliest pending deadline has passed
// (deadline index, REV-11). Each contribution is processed in its own
// CacheContext and committed only when every step succeeded, so a failing
// step can never leave a half-applied payout, slash or stake return behind
// to be repeated on the next block (REV-01, REV-06). Errors are logged and
// never returned: an EndBlock error would halt the chain.
func (k Keeper) ProcessDeadlines(ctx context.Context) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	currentEpoch := sdkCtx.BlockHeight()

	params, err := k.Params.Get(ctx)
	if err != nil {
		sdkCtx.Logger().Error("reveal: failed to load params for deadlines", "error", err)
		return nil
	}

	var due []uint64
	rng := new(collections.Range[collections.Pair[int64, uint64]]).
		EndInclusive(collections.Join(currentEpoch, uint64(stdmath.MaxUint64)))
	err = k.ContributionDeadlines.Walk(ctx, rng, func(key collections.Pair[int64, uint64]) (bool, error) {
		due = append(due, key.K2())
		return len(due) >= maxDeadlineContributionsPerBlock, nil
	})
	if err != nil {
		sdkCtx.Logger().Error("reveal: failed to walk deadline index", "error", err)
		return nil
	}

	for _, contribID := range due {
		contrib, err := k.Contribution.Get(ctx, contribID)
		if err != nil || contrib.Status != types.ContributionStatus_CONTRIBUTION_STATUS_IN_PROGRESS {
			// Orphaned or finished: drop the stale index entry.
			if cErr := k.clearDeadlineIndex(ctx, contribID); cErr != nil {
				sdkCtx.Logger().Error("reveal: failed to clear deadline index", "contribution_id", contribID, "error", cErr)
			}
			continue
		}

		cacheCtx, write := sdkCtx.CacheContext()
		if err := k.processContributionDeadlines(cacheCtx, &contrib, currentEpoch, &params); err != nil {
			sdkCtx.Logger().Error("failed to process deadlines; contribution left unchanged and retried next block",
				"contribution_id", contribID, "error", err)
			sdkCtx.EventManager().EmitEvent(sdk.NewEvent("contribution_deadline_failed",
				sdk.NewAttribute("contribution_id", fmt.Sprintf("%d", contribID)),
				sdk.NewAttribute("error", err.Error()),
			))
			continue
		}
		// Re-index even when nothing was due (stale entry after a param change).
		if err := k.saveContribution(cacheCtx, &contrib); err != nil {
			sdkCtx.Logger().Error("failed to save contribution after deadlines", "contribution_id", contribID, "error", err)
			continue
		}
		write()
	}

	k.RetryProjectTransitions(ctx)
	return nil
}

func (k Keeper) processContributionDeadlines(ctx context.Context, contrib *types.Contribution, currentEpoch int64, params *types.Params) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	for i := range contrib.Tranches {
		tranche := &contrib.Tranches[i]

		switch tranche.Status {
		case types.TrancheStatus_TRANCHE_STATUS_STAKING:
			if tranche.StakeDeadline > 0 && currentEpoch >= tranche.StakeDeadline {
				// Stake deadline expired — cancel contribution (partial completion not supported)
				if err := k.handleStakeTimeout(ctx, contrib, tranche, params); err != nil {
					return err
				}
				sdkCtx.EventManager().EmitEvent(
					sdk.NewEvent("tranche_cancelled",
						sdk.NewAttribute("contribution_id", fmt.Sprintf("%d", contrib.Id)),
						sdk.NewAttribute("tranche_id", fmt.Sprintf("%d", tranche.Id)),
						sdk.NewAttribute("reason", "stake deadline expired"),
					),
				)
				return nil // contribution is now cancelled, stop processing
			}

		case types.TrancheStatus_TRANCHE_STATUS_BACKED:
			if tranche.RevealDeadline > 0 && currentEpoch >= tranche.RevealDeadline {
				// Capture slashed amount before unlock/burn consumes BondRemaining.
				slashed := contrib.BondRemaining
				// Reveal deadline expired — fail tranche, slash full remaining bond
				if err := k.handleRevealTimeout(ctx, contrib, tranche); err != nil {
					return err
				}
				sdkCtx.EventManager().EmitEvent(
					sdk.NewEvent("tranche_failed",
						sdk.NewAttribute("contribution_id", fmt.Sprintf("%d", contrib.Id)),
						sdk.NewAttribute("tranche_id", fmt.Sprintf("%d", tranche.Id)),
						sdk.NewAttribute("reason", "reveal deadline expired"),
						sdk.NewAttribute("bond_slashed", slashed.String()),
					),
				)
				return nil // contribution is now cancelled
			}

		case types.TrancheStatus_TRANCHE_STATUS_REVEALED:
			if tranche.VerificationDeadline > 0 && currentEpoch >= tranche.VerificationDeadline {
				// Auto-tally votes
				if err := k.handleVerificationDeadline(ctx, contrib, tranche, params); err != nil {
					return err
				}
				// The caller saves the contribution (status may have changed).
				return nil // re-process on next block for any new status changes
			}

		case types.TrancheStatus_TRANCHE_STATUS_DISPUTED:
			disputeDeadline := tranche.VerificationDeadline + params.DisputeResolutionEpochs
			if currentEpoch >= disputeDeadline {
				// Auto-REJECT: council missed resolution window
				if err := k.handleDisputeTimeout(ctx, contrib, tranche); err != nil {
					return err
				}
				sdkCtx.EventManager().EmitEvent(
					sdk.NewEvent("dispute_resolved",
						sdk.NewAttribute("contribution_id", fmt.Sprintf("%d", contrib.Id)),
						sdk.NewAttribute("tranche_id", fmt.Sprintf("%d", tranche.Id)),
						sdk.NewAttribute("verdict", "REJECT"),
						sdk.NewAttribute("reason", "dispute resolution timeout (auto-REJECT)"),
					),
				)
				return nil // contribution is now cancelled
			}
		}
	}

	return nil
}

// handleStakeTimeout handles a tranche whose stake deadline has expired.
// Cancels the entire contribution (partial completion not supported).
func (k Keeper) handleStakeTimeout(ctx context.Context, contrib *types.Contribution, tranche *types.RevealTranche, params *types.Params) error {
	// Return all stakes for this tranche
	if err := k.returnTrancheStakes(ctx, contrib.Id, tranche.Id); err != nil {
		return err
	}

	// Burn accumulated holdback
	contrib.HoldbackAmount = math.ZeroInt()

	// Return remaining bond to contributor (not their fault community didn't stake)
	contributorAddr, err := k.addressCodec.StringToBytes(contrib.Contributor)
	if err != nil {
		return err
	}
	if _, err := k.unlockOrGone(ctx, sdk.AccAddress(contributorAddr), contrib.BondRemaining); err != nil {
		return err
	}
	contrib.BondRemaining = math.ZeroInt()

	tranche.Status = types.TrancheStatus_TRANCHE_STATUS_CANCELLED
	return k.cancelContribution(ctx, contrib)
}

// cancelContribution cancels every LOCKED tranche, moves the contribution to
// CANCELLED and saves it together with its status index.
func (k Keeper) cancelContribution(ctx context.Context, contrib *types.Contribution) error {
	if err := k.ContributionsByStatus.Remove(ctx, collections.Join(int32(contrib.Status), contrib.Id)); err != nil {
		return err
	}
	for i := range contrib.Tranches {
		if contrib.Tranches[i].Status == types.TrancheStatus_TRANCHE_STATUS_LOCKED {
			contrib.Tranches[i].Status = types.TrancheStatus_TRANCHE_STATUS_CANCELLED
		}
	}
	contrib.Status = types.ContributionStatus_CONTRIBUTION_STATUS_CANCELLED
	if err := k.saveContribution(ctx, contrib); err != nil {
		return err
	}
	return k.ContributionsByStatus.Set(ctx, collections.Join(int32(contrib.Status), contrib.Id))
}

// handleRevealTimeout handles a backed tranche where contributor failed to reveal.
// Slashes entire remaining bond, burns holdback, cancels contribution.
func (k Keeper) handleRevealTimeout(ctx context.Context, contrib *types.Contribution, tranche *types.RevealTranche) error {
	// Return all stakes
	if err := k.returnTrancheStakes(ctx, contrib.Id, tranche.Id); err != nil {
		return err
	}

	contributorAddr, err := k.addressCodec.StringToBytes(contrib.Contributor)
	if err != nil {
		return err
	}

	// Slash entire remaining bond. A zeroed contributor's bond is already
	// burned by x/rep, so there is nothing left to burn.
	if contrib.BondRemaining.IsPositive() {
		if _, err := k.burnLockedOrGone(ctx, sdk.AccAddress(contributorAddr), contrib.BondRemaining); err != nil {
			return err
		}
		contrib.BondRemaining = math.ZeroInt()
	}

	// Burn accumulated holdback (don't mint it)
	contrib.HoldbackAmount = math.ZeroInt()

	// Deduct reputation for failing to reveal
	if err := k.repKeeper.DeductReputation(ctx, sdk.AccAddress(contributorAddr), "reveal", math.LegacyNewDec(20)); err != nil {
		return err
	}

	tranche.Status = types.TrancheStatus_TRANCHE_STATUS_FAILED
	return k.cancelContribution(ctx, contrib)
}

// handleVerificationDeadline auto-tallies votes when verification period ends.
func (k Keeper) handleVerificationDeadline(ctx context.Context, contrib *types.Contribution, tranche *types.RevealTranche, params *types.Params) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Tally votes
	yesWeight, noWeight, voteCount, err := k.tallyVotes(ctx, contrib.Id, tranche.Id)
	if err != nil {
		return err
	}

	effectiveMin := EffectiveMinVotes(params.MinVerificationVotes, tranche.StakeThreshold)

	// REVEAL-4 fix: Cap verification deadline extensions to a maximum of 3.
	// Without this cap, parameter changes to VerificationPeriodEpochs could allow
	// infinite extensions by resetting the arithmetic relationship between
	// the current deadline and the original deadline.
	// NOTE: Ideally an extension_count field would be added to the RevealTranche proto,
	// but for now we enforce the cap via a time-based check: the deadline can never
	// exceed RevealedAt + (maxExtensions+1) * VerificationPeriodEpochs.
	const maxVerificationExtensions = 3
	if voteCount < effectiveMin && tranche.VerificationDeadline > 0 {
		maxDeadline := tranche.RevealedAt + int64(maxVerificationExtensions+1)*params.VerificationPeriodEpochs
		if tranche.VerificationDeadline+params.VerificationPeriodEpochs <= maxDeadline {
			// Extension allowed — still within max extension cap
			tranche.VerificationDeadline = tranche.VerificationDeadline + params.VerificationPeriodEpochs
			return nil
		}
		// Already at max extensions, proceed with tally as-is
	}

	totalWeight := yesWeight.Add(noWeight)
	if totalWeight.IsZero() {
		// No votes at all — mark disputed
		tranche.Status = types.TrancheStatus_TRANCHE_STATUS_DISPUTED
		sdkCtx.EventManager().EmitEvent(
			sdk.NewEvent("tranche_disputed",
				sdk.NewAttribute("contribution_id", fmt.Sprintf("%d", contrib.Id)),
				sdk.NewAttribute("tranche_id", fmt.Sprintf("%d", tranche.Id)),
				sdk.NewAttribute("reason", "no votes cast"),
			),
		)
		return nil
	}

	// Calculate pass/fail
	yesRatio := math.LegacyNewDecFromInt(yesWeight).Quo(math.LegacyNewDecFromInt(totalWeight))
	passed := yesRatio.GTE(params.VerificationThreshold) && voteCount >= effectiveMin

	if passed {
		// Verification passed — confirm tranche
		if err := k.confirmTranche(ctx, contrib, tranche, params); err != nil {
			return err
		}
	} else {
		// Verification failed — mark disputed
		tranche.Status = types.TrancheStatus_TRANCHE_STATUS_DISPUTED

		sdkCtx.EventManager().EmitEvent(
			sdk.NewEvent("tranche_disputed",
				sdk.NewAttribute("contribution_id", fmt.Sprintf("%d", contrib.Id)),
				sdk.NewAttribute("tranche_id", fmt.Sprintf("%d", tranche.Id)),
				sdk.NewAttribute("yes_weight", yesWeight.String()),
				sdk.NewAttribute("no_weight", noWeight.String()),
				sdk.NewAttribute("vote_count", fmt.Sprintf("%d", voteCount)),
			),
		)
	}

	return nil
}

// handleDisputeTimeout handles auto-REJECT when council misses dispute resolution window.
// Stakes are returned first, then 50% of the remaining bond is burned and
// the rest released. With a zeroed contributor the bond is already gone and
// the stakers still get their DREAM back (RVL-2 addendum).
func (k Keeper) handleDisputeTimeout(ctx context.Context, contrib *types.Contribution, tranche *types.RevealTranche) error {
	contributorAddr, err := k.addressCodec.StringToBytes(contrib.Contributor)
	if err != nil {
		return err
	}

	// Return stakes
	if err := k.returnTrancheStakes(ctx, contrib.Id, tranche.Id); err != nil {
		return err
	}
	if err := k.deleteTrancheVotes(ctx, contrib.Id, tranche.Id); err != nil {
		return err
	}

	// Slash 50% of remaining bond, release the rest
	if _, err := k.slashHalfBondAndRelease(ctx, contrib, sdk.AccAddress(contributorAddr)); err != nil {
		return err
	}

	// Burn holdback
	contrib.HoldbackAmount = math.ZeroInt()

	// Deduct reputation for dispute timeout
	if err := k.repKeeper.DeductReputation(ctx, sdk.AccAddress(contributorAddr), "reveal", math.LegacyNewDec(10)); err != nil {
		return err
	}

	tranche.Status = types.TrancheStatus_TRANCHE_STATUS_FAILED
	return k.cancelContribution(ctx, contrib)
}

// tallyVotes aggregates yes/no weights and vote count for a tranche.
func (k Keeper) tallyVotes(ctx context.Context, contributionID uint64, trancheID uint32) (yesWeight, noWeight math.Int, voteCount uint32, err error) {
	yesWeight = math.ZeroInt()
	noWeight = math.ZeroInt()
	voteCount = 0

	trancheKey := TrancheKey(contributionID, trancheID)
	err = k.VotesByTranche.Walk(ctx,
		collections.NewPrefixedPairRange[string, string](trancheKey),
		func(key collections.Pair[string, string]) (bool, error) {
			vote, err := k.Vote.Get(ctx, key.K2())
			if err != nil {
				return true, err
			}
			if vote.ValueConfirmed {
				yesWeight = yesWeight.Add(vote.StakeWeight)
			} else {
				noWeight = noWeight.Add(vote.StakeWeight)
			}
			voteCount++
			return false, nil
		},
	)
	return
}

// RevealReputationCapPerTranche caps the reputation one verified tranche
// grants on the "reveal" tag. It matches x/rep's default
// max_reputation_gain_per_epoch so a single tranche can never jump a
// contributor past the trust-level thresholds on its own (REV-03).
var RevealReputationCapPerTranche = math.LegacyNewDec(50)

// udreamPerDream converts micro-DREAM (the unit of stake_threshold) to DREAM.
const udreamPerDream = 1_000_000

// trancheReputation returns the reputation granted for a verified tranche:
// (stake_threshold in DREAM / 1000) * (avg_quality / 5), capped at
// RevealReputationCapPerTranche.
func trancheReputation(stakeThreshold math.Int, avgQuality math.LegacyDec) math.LegacyDec {
	rep := math.LegacyNewDecFromInt(stakeThreshold).
		QuoInt64(udreamPerDream).
		QuoInt64(1000).
		Mul(avgQuality).
		QuoInt64(5)
	if rep.GT(RevealReputationCapPerTranche) {
		rep = RevealReputationCapPerTranche
	}
	return rep
}

// confirmTranche handles verification success: return stakes, payout (with
// holdback), grant reputation, and advance to next tranche or complete. The
// mint runs after the fallible stake returns; the caller runs the whole
// step in a CacheContext so nothing is ever applied twice.
func (k Keeper) confirmTranche(ctx context.Context, contrib *types.Contribution, tranche *types.RevealTranche, params *types.Params) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	currentEpoch := sdkCtx.BlockHeight()

	contributorAddr, err := k.addressCodec.StringToBytes(contrib.Contributor)
	if err != nil {
		return err
	}

	// Return stakes to stakers
	if err := k.returnTrancheStakes(ctx, contrib.Id, tranche.Id); err != nil {
		return err
	}

	// Average quality from verification votes, then drop the votes: the
	// tranche is terminal and only the aggregate is kept (REV-10).
	avgQuality := k.calculateAvgQuality(ctx, contrib.Id, tranche.Id)
	if err := k.deleteTrancheVotes(ctx, contrib.Id, tranche.Id); err != nil {
		return err
	}

	// Calculate payout with holdback
	holdback := params.PayoutHoldbackRate.MulInt(tranche.StakeThreshold).TruncateInt()
	immediatePayout := tranche.StakeThreshold.Sub(holdback)

	// Mint DREAM to contributor (immediate portion)
	if immediatePayout.IsPositive() {
		if err := k.repKeeper.MintDREAM(ctx, sdk.AccAddress(contributorAddr), immediatePayout); err != nil {
			return err
		}
	}

	// Accumulate holdback
	contrib.HoldbackAmount = contrib.HoldbackAmount.Add(holdback)

	// Grant reputation scaled by average quality rating from verification votes
	repScore := trancheReputation(tranche.StakeThreshold, avgQuality)
	if repScore.IsPositive() {
		// Through x/rep's per-epoch cap, on top of the per-tranche cap in
		// trancheReputation.
		var getErr error
		_, getErr = k.repKeeper.AddReputationCapped(ctx, sdk.AccAddress(contributorAddr), "reveal", repScore)
		if getErr != nil {
			sdk.UnwrapSDKContext(ctx).Logger().Error("AddReputationCapped failed", "err", getErr)
		}
	}

	// Mark tranche as verified
	tranche.Status = types.TrancheStatus_TRANCHE_STATUS_VERIFIED
	tranche.VerifiedAt = currentEpoch

	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent("tranche_verified",
			sdk.NewAttribute("contribution_id", fmt.Sprintf("%d", contrib.Id)),
			sdk.NewAttribute("tranche_id", fmt.Sprintf("%d", tranche.Id)),
			sdk.NewAttribute("avg_quality_rating", avgQuality.String()),
			sdk.NewAttribute("dream_paid", immediatePayout.String()),
			sdk.NewAttribute("holdback", holdback.String()),
			sdk.NewAttribute("reputation_granted", repScore.String()),
		),
	)

	// Check if all tranches are now verified
	allVerified := true
	nextTranche := -1
	for i := range contrib.Tranches {
		if contrib.Tranches[i].Status != types.TrancheStatus_TRANCHE_STATUS_VERIFIED {
			allVerified = false
			if contrib.Tranches[i].Status == types.TrancheStatus_TRANCHE_STATUS_LOCKED && nextTranche == -1 {
				nextTranche = i
			}
		}
	}

	if allVerified {
		// All tranches complete — release holdback, return bond, transition to project
		if err := k.completeContribution(ctx, contrib); err != nil {
			return err
		}
	} else if nextTranche >= 0 {
		// Unlock next tranche for staking
		contrib.Tranches[nextTranche].Status = types.TrancheStatus_TRANCHE_STATUS_STAKING
		contrib.Tranches[nextTranche].StakeDeadline = currentEpoch + params.StakeDeadlineEpochs
		contrib.CurrentTranche = commontypes.Saturate[uint32](nextTranche)
	}

	return nil
}

// completeContribution handles the final phase: release holdback, return bond, create project.
// A failed holdback mint (e.g. the per-epoch mint cap) returns an error: the
// caller's CacheContext rolls the whole verification back and the EndBlocker
// retries it on the next block, so the holdback is never silently lost
// (REV-08). The status index is written together with the record.
func (k Keeper) completeContribution(ctx context.Context, contrib *types.Contribution) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	contributorAddr, err := k.addressCodec.StringToBytes(contrib.Contributor)
	if err != nil {
		return err
	}

	// Release holdback to contributor
	if contrib.HoldbackAmount.IsPositive() {
		if err := k.repKeeper.MintDREAM(ctx, sdk.AccAddress(contributorAddr), contrib.HoldbackAmount); err != nil {
			return fmt.Errorf("mint holdback on completion: %w", err)
		}
		contrib.HoldbackAmount = math.ZeroInt()
	}

	// Return bond to contributor (already gone if the contributor was zeroed)
	if _, err := k.unlockOrGone(ctx, sdk.AccAddress(contributorAddr), contrib.BondRemaining); err != nil {
		return err
	}
	contrib.BondRemaining = math.ZeroInt()

	// Transition to x/rep project. A failure is not value-moving: the
	// contribution completes anyway with TransitionedToProject = false and
	// is queued for retry by the EndBlocker (REV-07).
	if err := k.tryProjectTransition(ctx, contrib); err != nil {
		sdkCtx.Logger().Error("failed to create project on transition; queued for retry", "error", err)
		if qErr := k.queueProjectTransition(ctx, contrib.Id, sdkCtx.BlockHeight()+projectTransitionRetryBlocks); qErr != nil {
			return qErr
		}
	}

	if err := k.ContributionsByStatus.Remove(ctx, collections.Join(int32(contrib.Status), contrib.Id)); err != nil {
		return err
	}
	contrib.Status = types.ContributionStatus_CONTRIBUTION_STATUS_COMPLETED
	if err := k.ContributionsByStatus.Set(ctx, collections.Join(int32(contrib.Status), contrib.Id)); err != nil {
		return err
	}

	// Emit completion event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent("contribution_completed",
			sdk.NewAttribute("contribution_id", fmt.Sprintf("%d", contrib.Id)),
			sdk.NewAttribute("project_id", fmt.Sprintf("%d", contrib.ProjectId)),
			sdk.NewAttribute("transitioned_to_project", fmt.Sprintf("%t", contrib.TransitionedToProject)),
		),
	)

	return nil
}

// calculateAvgQuality computes the stake-weighted average quality rating for a tranche.
func (k Keeper) calculateAvgQuality(ctx context.Context, contributionID uint64, trancheID uint32) math.LegacyDec {
	totalWeight := math.ZeroInt()
	weightedSum := math.ZeroInt()

	trancheKey := TrancheKey(contributionID, trancheID)
	if err := k.VotesByTranche.Walk(ctx,
		collections.NewPrefixedPairRange[string, string](trancheKey),
		func(key collections.Pair[string, string]) (bool, error) {
			vote, err := k.Vote.Get(ctx, key.K2())
			if err != nil {
				return false, nil
			}
			weightedSum = weightedSum.Add(vote.StakeWeight.MulRaw(int64(vote.QualityRating)))
			totalWeight = totalWeight.Add(vote.StakeWeight)
			return false, nil
		},
	); err != nil {
		sdk.UnwrapSDKContext(ctx).Logger().Error("calculateAvgQuality: VotesByTranche.Walk failed", "err", err)
	}

	if totalWeight.IsZero() {
		return math.LegacyNewDec(3) // default mid rating
	}
	return math.LegacyNewDecFromInt(weightedSum).Quo(math.LegacyNewDecFromInt(totalWeight))
}

// Project transition retry (REV-07).
const (
	// projectTransitionRetryBlocks is the base delay between attempts; the
	// n-th retry waits n times as long.
	projectTransitionRetryBlocks = int64(100)
	// maxProjectTransitionAttempts bounds the retries per contribution.
	maxProjectTransitionAttempts = uint32(10)
	// maxProjectTransitionsPerBlock caps the retry pass.
	maxProjectTransitionsPerBlock = 5
)

// tryProjectTransition creates the community-owned x/rep project of a
// completed contribution in a cache context, so a failure leaves no partial
// rep state. Reveal approval always comes from the Commons Council, so the
// project is filed under it.
func (k Keeper) tryProjectTransition(ctx context.Context, contrib *types.Contribution) error {
	contributorAddr, err := k.addressCodec.StringToBytes(contrib.Contributor)
	if err != nil {
		return err
	}
	cacheCtx, write := sdk.UnwrapSDKContext(ctx).CacheContext()
	projectID, err := k.repKeeper.CreateProject(
		cacheCtx,
		sdk.AccAddress(contributorAddr),
		contrib.ProjectName,
		contrib.Description,
		[]string{"reveal"}, // tags
		reptypes.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, // default category
		"commons",      // council that approved the contribution
		math.ZeroInt(), // requestedBudget (reveal-sourced projects are pre-funded)
		math.ZeroInt(), // requestedSpark
		false,          // not permissionless — reveal-transitioned project
	)
	if err != nil {
		return err
	}
	write()
	contrib.TransitionedToProject = true
	contrib.ProjectId = projectID
	return nil
}

// queueProjectTransition files a contribution for a project-creation retry
// at height `at`.
func (k Keeper) queueProjectTransition(ctx context.Context, id uint64, at int64) error {
	return k.ProjectTransitionQueue.Set(ctx, collections.Join(at, id))
}

// RetryProjectTransitions retries the x/rep project creation of completed
// contributions whose transition failed, at most
// maxProjectTransitionsPerBlock per block. Each failure doubles back with a
// longer delay; after maxProjectTransitionAttempts the contribution is
// dropped from the queue (event contribution_project_transition_abandoned)
// and keeps TransitionedToProject = false. Errors are logged, never returned.
func (k Keeper) RetryProjectTransitions(ctx context.Context) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	height := sdkCtx.BlockHeight()
	rng := new(collections.Range[collections.Pair[int64, uint64]]).
		EndInclusive(collections.Join(height, uint64(stdmath.MaxUint64)))
	var due []collections.Pair[int64, uint64]
	if err := k.ProjectTransitionQueue.Walk(ctx, rng, func(key collections.Pair[int64, uint64]) (bool, error) {
		due = append(due, key)
		return len(due) >= maxProjectTransitionsPerBlock, nil
	}); err != nil {
		sdkCtx.Logger().Error("reveal: failed to walk project transition queue", "error", err)
		return
	}
	for _, key := range due {
		id := key.K2()
		if err := k.ProjectTransitionQueue.Remove(ctx, key); err != nil {
			sdk.UnwrapSDKContext(ctx).Logger().Error("RetryProjectTransitions: ProjectTransitionQueue.Remove failed", "err", err)
		}
		contrib, err := k.Contribution.Get(ctx, id)
		if err != nil || contrib.TransitionedToProject ||
			contrib.Status != types.ContributionStatus_CONTRIBUTION_STATUS_COMPLETED {
			if err := k.ProjectTransitionAttempts.Remove(ctx, id); err != nil {
				sdk.UnwrapSDKContext(ctx).Logger().Error("RetryProjectTransitions: ProjectTransitionAttempts.Remove failed", "err", err)
			}
			continue
		}
		if err := k.tryProjectTransition(ctx, &contrib); err != nil {
			attempts, getErr := k.ProjectTransitionAttempts.Get(ctx, id)
			if getErr != nil && !errors.Is(getErr, collections.ErrNotFound) {
				sdk.UnwrapSDKContext(ctx).Logger().Error("RetryProjectTransitions: ProjectTransitionAttempts.Get failed", "err", getErr)
			}
			attempts++
			if attempts >= maxProjectTransitionAttempts {
				if err := k.ProjectTransitionAttempts.Remove(ctx, id); err != nil {
					sdk.UnwrapSDKContext(ctx).Logger().Error("RetryProjectTransitions: ProjectTransitionAttempts.Remove failed", "err", err)
				}
				sdkCtx.EventManager().EmitEvent(sdk.NewEvent("contribution_project_transition_abandoned",
					sdk.NewAttribute("contribution_id", fmt.Sprintf("%d", id)),
					sdk.NewAttribute("error", err.Error()),
				))
				continue
			}
			if err := k.ProjectTransitionAttempts.Set(ctx, id, attempts); err != nil {
				sdk.UnwrapSDKContext(ctx).Logger().Error("RetryProjectTransitions: ProjectTransitionAttempts.Set failed", "err", err)
			}
			if err := k.queueProjectTransition(ctx, id, height+projectTransitionRetryBlocks*int64(attempts+1)); err != nil {
				sdk.UnwrapSDKContext(ctx).Logger().Error("RetryProjectTransitions: k.queueProjectTransition failed", "err", err)
			}
			continue
		}
		if err := k.Contribution.Set(ctx, id, contrib); err != nil {
			sdkCtx.Logger().Error("reveal: failed to save transitioned contribution", "contribution_id", id, "error", err)
			continue
		}
		if err := k.ProjectTransitionAttempts.Remove(ctx, id); err != nil {
			sdk.UnwrapSDKContext(ctx).Logger().Error("RetryProjectTransitions: ProjectTransitionAttempts.Remove failed", "err", err)
		}
		sdkCtx.EventManager().EmitEvent(sdk.NewEvent("contribution_project_transitioned",
			sdk.NewAttribute("contribution_id", fmt.Sprintf("%d", id)),
			sdk.NewAttribute("project_id", fmt.Sprintf("%d", contrib.ProjectId)),
		))
	}
}

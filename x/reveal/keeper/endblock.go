package keeper

import (
	"context"
	"fmt"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	reptypes "sparkdream/x/rep/types"
	"sparkdream/x/reveal/types"
)

// ProcessDeadlines runs every block and enforces all deadline-based transitions.
// It iterates over contributions with status IN_PROGRESS and checks each active tranche.
func (k Keeper) ProcessDeadlines(ctx context.Context) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	currentEpoch := sdkCtx.BlockHeight()

	params, err := k.Params.Get(ctx)
	if err != nil {
		return err
	}

	// Iterate all IN_PROGRESS contributions
	var contribIDs []uint64
	err = k.ContributionsByStatus.Walk(ctx,
		collections.NewPrefixedPairRange[int32, uint64](int32(types.ContributionStatus_CONTRIBUTION_STATUS_IN_PROGRESS)),
		func(key collections.Pair[int32, uint64]) (bool, error) {
			contribIDs = append(contribIDs, key.K2())
			return false, nil
		},
	)
	if err != nil {
		return err
	}

	for _, contribID := range contribIDs {
		contrib, err := k.Contribution.Get(ctx, contribID)
		if err != nil {
			continue
		}
		if err := k.processContributionDeadlines(ctx, &contrib, currentEpoch, &params); err != nil {
			sdkCtx.Logger().Error("failed to process deadlines", "contribution_id", contribID, "error", err)
		}
	}

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
				// Save after tally (status may have changed)
				if err := k.Contribution.Set(ctx, contrib.Id, *contrib); err != nil {
					return err
				}
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
	if contrib.BondRemaining.IsPositive() {
		if err := k.repKeeper.UnlockDREAM(ctx, sdk.AccAddress(contributorAddr), contrib.BondRemaining); err != nil {
			return err
		}
		contrib.BondRemaining = math.ZeroInt()
	}

	// Cancel all tranches and contribution
	if err := k.ContributionsByStatus.Remove(ctx, collections.Join(int32(contrib.Status), contrib.Id)); err != nil {
		return err
	}

	tranche.Status = types.TrancheStatus_TRANCHE_STATUS_CANCELLED
	for i := range contrib.Tranches {
		if contrib.Tranches[i].Status == types.TrancheStatus_TRANCHE_STATUS_LOCKED {
			contrib.Tranches[i].Status = types.TrancheStatus_TRANCHE_STATUS_CANCELLED
		}
	}
	contrib.Status = types.ContributionStatus_CONTRIBUTION_STATUS_CANCELLED

	if err := k.Contribution.Set(ctx, contrib.Id, *contrib); err != nil {
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

	// Slash entire remaining bond
	if contrib.BondRemaining.IsPositive() {
		if err := k.repKeeper.UnlockDREAM(ctx, sdk.AccAddress(contributorAddr), contrib.BondRemaining); err != nil {
			return err
		}
		if err := k.repKeeper.BurnDREAM(ctx, sdk.AccAddress(contributorAddr), contrib.BondRemaining); err != nil {
			return err
		}
		contrib.BondRemaining = math.ZeroInt()
	}

	// Burn accumulated holdback (don't mint it)
	contrib.HoldbackAmount = math.ZeroInt()

	// Deduct reputation for failing to reveal
	_ = k.repKeeper.DeductReputation(ctx, sdk.AccAddress(contributorAddr), "reveal", math.LegacyNewDec(20))

	// Cancel all tranches and contribution
	if err := k.ContributionsByStatus.Remove(ctx, collections.Join(int32(contrib.Status), contrib.Id)); err != nil {
		return err
	}

	tranche.Status = types.TrancheStatus_TRANCHE_STATUS_FAILED
	for i := range contrib.Tranches {
		if contrib.Tranches[i].Status == types.TrancheStatus_TRANCHE_STATUS_LOCKED {
			contrib.Tranches[i].Status = types.TrancheStatus_TRANCHE_STATUS_CANCELLED
		}
	}
	contrib.Status = types.ContributionStatus_CONTRIBUTION_STATUS_CANCELLED

	if err := k.Contribution.Set(ctx, contrib.Id, *contrib); err != nil {
		return err
	}
	return k.ContributionsByStatus.Set(ctx, collections.Join(int32(contrib.Status), contrib.Id))
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

		sdkCtx.EventManager().EmitEvent(
			sdk.NewEvent("tranche_verified",
				sdk.NewAttribute("contribution_id", fmt.Sprintf("%d", contrib.Id)),
				sdk.NewAttribute("tranche_id", fmt.Sprintf("%d", tranche.Id)),
			),
		)
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
func (k Keeper) handleDisputeTimeout(ctx context.Context, contrib *types.Contribution, tranche *types.RevealTranche) error {
	contributorAddr, err := k.addressCodec.StringToBytes(contrib.Contributor)
	if err != nil {
		return err
	}

	// Slash 50% of remaining bond
	slashAmount := contrib.BondRemaining.Quo(math.NewInt(2))
	if slashAmount.IsPositive() {
		if err := k.repKeeper.UnlockDREAM(ctx, sdk.AccAddress(contributorAddr), slashAmount); err != nil {
			return err
		}
		if err := k.repKeeper.BurnDREAM(ctx, sdk.AccAddress(contributorAddr), slashAmount); err != nil {
			return err
		}
		contrib.BondRemaining = contrib.BondRemaining.Sub(slashAmount)
	}

	// Return remaining bond
	if contrib.BondRemaining.IsPositive() {
		if err := k.repKeeper.UnlockDREAM(ctx, sdk.AccAddress(contributorAddr), contrib.BondRemaining); err != nil {
			return err
		}
		contrib.BondRemaining = math.ZeroInt()
	}

	// Burn holdback
	contrib.HoldbackAmount = math.ZeroInt()

	// Return stakes
	if err := k.returnTrancheStakes(ctx, contrib.Id, tranche.Id); err != nil {
		return err
	}

	// Deduct reputation for dispute timeout
	_ = k.repKeeper.DeductReputation(ctx, sdk.AccAddress(contributorAddr), "reveal", math.LegacyNewDec(10))

	// Fail tranche, cancel remaining
	if err := k.ContributionsByStatus.Remove(ctx, collections.Join(int32(contrib.Status), contrib.Id)); err != nil {
		return err
	}

	tranche.Status = types.TrancheStatus_TRANCHE_STATUS_FAILED
	for i := range contrib.Tranches {
		if contrib.Tranches[i].Status == types.TrancheStatus_TRANCHE_STATUS_LOCKED {
			contrib.Tranches[i].Status = types.TrancheStatus_TRANCHE_STATUS_CANCELLED
		}
	}
	contrib.Status = types.ContributionStatus_CONTRIBUTION_STATUS_CANCELLED

	if err := k.Contribution.Set(ctx, contrib.Id, *contrib); err != nil {
		return err
	}
	return k.ContributionsByStatus.Set(ctx, collections.Join(int32(contrib.Status), contrib.Id))
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

// confirmTranche handles verification success: payout (with holdback), return stakes,
// grant reputation, and advance to next tranche or complete.
func (k Keeper) confirmTranche(ctx context.Context, contrib *types.Contribution, tranche *types.RevealTranche, params *types.Params) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	currentEpoch := sdkCtx.BlockHeight()

	contributorAddr, err := k.addressCodec.StringToBytes(contrib.Contributor)
	if err != nil {
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

	// Return stakes to stakers
	if err := k.returnTrancheStakes(ctx, contrib.Id, tranche.Id); err != nil {
		return err
	}

	// Grant reputation scaled by average quality rating from verification votes
	avgQuality := k.calculateAvgQuality(ctx, contrib.Id, tranche.Id)
	repScore := math.LegacyNewDecFromInt(tranche.StakeThreshold).
		Quo(math.LegacyNewDec(1000)).
		Mul(avgQuality).
		Quo(math.LegacyNewDec(5))
	if repScore.IsPositive() {
		_ = k.repKeeper.AddReputation(ctx, sdk.AccAddress(contributorAddr), "reveal", repScore)
	}

	// Mark tranche as verified
	tranche.Status = types.TrancheStatus_TRANCHE_STATUS_VERIFIED
	tranche.VerifiedAt = currentEpoch

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
		contrib.CurrentTranche = uint32(nextTranche)
	}

	return nil
}

// completeContribution handles the final phase: release holdback, return bond, create project.
func (k Keeper) completeContribution(ctx context.Context, contrib *types.Contribution) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	contributorAddr, err := k.addressCodec.StringToBytes(contrib.Contributor)
	if err != nil {
		return err
	}

	// Transition status + index BEFORE any mint/unlock that may fail due to epoch caps
	// or transient errors — the contribution must still be marked complete.
	if err := k.ContributionsByStatus.Remove(ctx, collections.Join(int32(contrib.Status), contrib.Id)); err != nil {
		return err
	}
	contrib.Status = types.ContributionStatus_CONTRIBUTION_STATUS_COMPLETED
	if err := k.ContributionsByStatus.Set(ctx, collections.Join(int32(contrib.Status), contrib.Id)); err != nil {
		return err
	}

	// Release holdback to contributor (log-only on failure: contribution is already COMPLETED)
	if contrib.HoldbackAmount.IsPositive() {
		if err := k.repKeeper.MintDREAM(ctx, sdk.AccAddress(contributorAddr), contrib.HoldbackAmount); err != nil {
			sdkCtx.Logger().Error("failed to mint holdback on completion", "error", err, "contribution_id", contrib.Id)
		} else {
			contrib.HoldbackAmount = math.ZeroInt()
		}
	}

	// Return bond to contributor
	if contrib.BondRemaining.IsPositive() {
		if err := k.repKeeper.UnlockDREAM(ctx, sdk.AccAddress(contributorAddr), contrib.BondRemaining); err != nil {
			return err
		}
		contrib.BondRemaining = math.ZeroInt()
	}

	// Transition to x/rep project
	creatorAddr := sdk.AccAddress(contributorAddr)
	projectID, err := k.repKeeper.CreateProject(
		ctx,
		creatorAddr,
		contrib.ProjectName,
		contrib.Description,
		[]string{"reveal"}, // tags
		reptypes.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, // default category
		"",             // council (determined by project governance)
		math.ZeroInt(), // requestedBudget (reveal-sourced projects are pre-funded)
		math.ZeroInt(), // requestedSpark
		false,          // not permissionless — reveal-transitioned project
	)
	if err != nil {
		// Log but don't fail — project creation can be retried
		sdkCtx.Logger().Error("failed to create project on transition", "error", err)
	} else {
		contrib.TransitionedToProject = true
		contrib.ProjectId = projectID
	}

	// Emit completion event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent("contribution_completed",
			sdk.NewAttribute("contribution_id", fmt.Sprintf("%d", contrib.Id)),
			sdk.NewAttribute("project_id", fmt.Sprintf("%d", contrib.ProjectId)),
		),
	)

	return nil
}

// calculateAvgQuality computes the stake-weighted average quality rating for a tranche.
func (k Keeper) calculateAvgQuality(ctx context.Context, contributionID uint64, trancheID uint32) math.LegacyDec {
	totalWeight := math.ZeroInt()
	weightedSum := math.ZeroInt()

	trancheKey := TrancheKey(contributionID, trancheID)
	_ = k.VotesByTranche.Walk(ctx,
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
	)

	if totalWeight.IsZero() {
		return math.LegacyNewDec(3) // default mid rating
	}
	return math.LegacyNewDecFromInt(weightedSum).Quo(math.LegacyNewDecFromInt(totalWeight))
}

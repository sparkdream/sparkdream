package keeper

import (
	"context"
	"fmt"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/reveal/types"
)

func (k msgServer) ResolveDispute(ctx context.Context, msg *types.MsgResolveDispute) (*types.MsgResolveDisputeResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Authority); err != nil {
		return nil, types.ErrUnauthorized.Wrapf("invalid authority address: %s", err)
	}

	// Dispute resolution must come through a Commons Council vote (or governance).
	// Individual committee members cannot unilaterally resolve — that would let a
	// single rogue member dictate ACCEPT/IMPROVE/REJECT verdicts.
	if !k.commonsKeeper.IsCouncilPolicyOrGov(ctx, msg.Authority, "commons") {
		return nil, types.ErrUnauthorized.Wrapf("unauthorized: must be governance or Commons Council")
	}

	// Verdict must not be UNSPECIFIED
	if msg.Verdict == types.DisputeVerdict_DISPUTE_VERDICT_UNSPECIFIED {
		return nil, types.ErrInvalidVerdict
	}

	// Get contribution
	contrib, err := k.Contribution.Get(ctx, msg.ContributionId)
	if err != nil {
		return nil, types.ErrContributionNotFound.Wrapf("contribution %d", msg.ContributionId)
	}

	// Must be IN_PROGRESS
	if contrib.Status != types.ContributionStatus_CONTRIBUTION_STATUS_IN_PROGRESS {
		return nil, types.ErrNotInProgress
	}

	// Get tranche
	tranche, err := GetTranche(&contrib, msg.TrancheId)
	if err != nil {
		return nil, err
	}

	// Tranche must be DISPUTED
	if tranche.Status != types.TrancheStatus_TRANCHE_STATUS_DISPUTED {
		return nil, types.ErrTrancheNotDisputed
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	currentEpoch := sdkCtx.BlockHeight()
	bondSlashed := math.ZeroInt()

	contributorAddr, err := k.addressCodec.StringToBytes(contrib.Contributor)
	if err != nil {
		return nil, err
	}

	params, err := k.Params.Get(ctx)
	if err != nil {
		return nil, err
	}

	switch msg.Verdict {
	case types.DisputeVerdict_DISPUTE_VERDICT_ACCEPT:
		// Code accepted — proceed to payout (same as verification pass)
		if err := k.confirmTranche(ctx, &contrib, tranche, &params); err != nil {
			return nil, err
		}

	case types.DisputeVerdict_DISPUTE_VERDICT_IMPROVE:
		// Code has merit but needs work — return to BACKED for re-reveal
		// Delete all verification votes for clean next round
		if err := k.deleteTrancheVotes(ctx, contrib.Id, msg.TrancheId); err != nil {
			return nil, err
		}

		tranche.Status = types.TrancheStatus_TRANCHE_STATUS_BACKED
		tranche.CodeUri = ""
		tranche.DocsUri = ""
		tranche.CommitHash = ""
		tranche.RevealedAt = 0
		tranche.VerificationDeadline = 0
		tranche.RevealDeadline = currentEpoch + params.RevealDeadlineEpochs

	case types.DisputeVerdict_DISPUTE_VERDICT_REJECT:
		// Hard fail — slash bond, burn holdback, cancel remaining
		slashAmount := contrib.BondRemaining.Quo(math.NewInt(2)) // 50% of remaining bond
		if slashAmount.IsPositive() {
			if err := k.repKeeper.UnlockDREAM(ctx, sdk.AccAddress(contributorAddr), slashAmount); err != nil {
				return nil, err
			}
			if err := k.repKeeper.BurnDREAM(ctx, sdk.AccAddress(contributorAddr), slashAmount); err != nil {
				return nil, err
			}
			contrib.BondRemaining = contrib.BondRemaining.Sub(slashAmount)
			bondSlashed = slashAmount
		}

		// Return remaining bond
		if contrib.BondRemaining.IsPositive() {
			if err := k.repKeeper.UnlockDREAM(ctx, sdk.AccAddress(contributorAddr), contrib.BondRemaining); err != nil {
				return nil, err
			}
			contrib.BondRemaining = math.ZeroInt()
		}

		// Burn accumulated holdback (don't mint it)
		contrib.HoldbackAmount = math.ZeroInt()

		// Return stakes for this tranche
		if err := k.returnTrancheStakes(ctx, contrib.Id, msg.TrancheId); err != nil {
			return nil, err
		}

		// Mark this tranche as FAILED
		tranche.Status = types.TrancheStatus_TRANCHE_STATUS_FAILED

		// Cancel all remaining LOCKED tranches
		for i := range contrib.Tranches {
			if contrib.Tranches[i].Status == types.TrancheStatus_TRANCHE_STATUS_LOCKED {
				contrib.Tranches[i].Status = types.TrancheStatus_TRANCHE_STATUS_CANCELLED
			}
		}

		// Deduct reputation for rejected dispute
		_ = k.repKeeper.DeductReputation(ctx, sdk.AccAddress(contributorAddr), "reveal", math.LegacyNewDec(10))

		// Update contribution status
		if err := k.ContributionsByStatus.Remove(ctx, collections.Join(int32(contrib.Status), contrib.Id)); err != nil {
			return nil, err
		}
		contrib.Status = types.ContributionStatus_CONTRIBUTION_STATUS_CANCELLED
		if err := k.ContributionsByStatus.Set(ctx, collections.Join(int32(contrib.Status), contrib.Id)); err != nil {
			return nil, err
		}
	}

	// Save updated contribution
	if err := k.Contribution.Set(ctx, contrib.Id, contrib); err != nil {
		return nil, err
	}

	// Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"dispute_resolved",
			sdk.NewAttribute("contribution_id", fmt.Sprintf("%d", contrib.Id)),
			sdk.NewAttribute("tranche_id", fmt.Sprintf("%d", msg.TrancheId)),
			sdk.NewAttribute("verdict", msg.Verdict.String()),
			sdk.NewAttribute("proposed_by", msg.Proposer),
			sdk.NewAttribute("reason", msg.Reason),
			sdk.NewAttribute("bond_slashed", bondSlashed.String()),
		),
	)

	return &types.MsgResolveDisputeResponse{}, nil
}

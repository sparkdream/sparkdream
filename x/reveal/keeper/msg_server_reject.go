package keeper

import (
	"context"
	"fmt"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/reveal/types"
)

func (k msgServer) Reject(ctx context.Context, msg *types.MsgReject) (*types.MsgRejectResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Authority); err != nil {
		return nil, types.ErrUnauthorized.Wrapf("invalid authority address: %s", err)
	}

	// Get the contribution
	contrib, err := k.Contribution.Get(ctx, msg.ContributionId)
	if err != nil {
		return nil, types.ErrContributionNotFound.Wrapf("contribution %d", msg.ContributionId)
	}

	// Must be in PROPOSED status
	if contrib.Status != types.ContributionStatus_CONTRIBUTION_STATUS_PROPOSED {
		return nil, types.ErrContributionNotProposed
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	currentEpoch := sdkCtx.BlockHeight()

	params, err := k.Params.Get(ctx)
	if err != nil {
		return nil, err
	}

	// Return bond to contributor
	contributorAddr, err := k.addressCodec.StringToBytes(contrib.Contributor)
	if err != nil {
		return nil, err
	}
	if contrib.BondRemaining.IsPositive() {
		if err := k.repKeeper.UnlockDREAM(ctx, sdk.AccAddress(contributorAddr), contrib.BondRemaining); err != nil {
			return nil, err
		}
	}

	// Remove old status index, update contribution
	if err := k.ContributionsByStatus.Remove(ctx, collections.Join(int32(contrib.Status), contrib.Id)); err != nil {
		return nil, err
	}

	contrib.Status = types.ContributionStatus_CONTRIBUTION_STATUS_CANCELLED
	contrib.ProposalEligibleAt = currentEpoch + params.ProposalCooldownEpochs

	// Cancel all tranches
	for i := range contrib.Tranches {
		contrib.Tranches[i].Status = types.TrancheStatus_TRANCHE_STATUS_CANCELLED
	}

	// Save updated contribution
	if err := k.Contribution.Set(ctx, contrib.Id, contrib); err != nil {
		return nil, err
	}
	if err := k.ContributionsByStatus.Set(ctx, collections.Join(int32(contrib.Status), contrib.Id)); err != nil {
		return nil, err
	}

	// Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"contribution_rejected",
			sdk.NewAttribute("contribution_id", fmt.Sprintf("%d", contrib.Id)),
			sdk.NewAttribute("proposed_by", msg.Proposer),
			sdk.NewAttribute("reason", msg.Reason),
			sdk.NewAttribute("proposal_eligible_at", fmt.Sprintf("%d", contrib.ProposalEligibleAt)),
		),
	)

	return &types.MsgRejectResponse{}, nil
}

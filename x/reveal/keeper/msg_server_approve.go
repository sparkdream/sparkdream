package keeper

import (
	"context"
	"fmt"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/reveal/types"
)

func (k msgServer) Approve(ctx context.Context, msg *types.MsgApprove) (*types.MsgApproveResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Authority); err != nil {
		return nil, types.ErrUnauthorized.Wrapf("invalid authority address: %s", err)
	}

	// Verify the caller is authorized: governance authority, council policy, or operations committee.
	// Reveal approvals come through Commons Council proposals, so the signer is
	// the council policy address, not the gov module address.
	if !k.commonsKeeper.IsCouncilAuthorized(ctx, msg.Authority, "commons", "operations") {
		return nil, types.ErrUnauthorized.Wrapf("unauthorized: must be governance, council, or operations committee")
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

	// Remove old status index, update contribution
	if err := k.ContributionsByStatus.Remove(ctx, collections.Join(int32(contrib.Status), contrib.Id)); err != nil {
		return nil, err
	}

	contrib.Status = types.ContributionStatus_CONTRIBUTION_STATUS_IN_PROGRESS
	contrib.ApprovedBy = msg.Proposer
	contrib.ApprovedAt = currentEpoch

	// Set tranche 0 to STAKING with deadline
	if len(contrib.Tranches) > 0 {
		contrib.Tranches[0].Status = types.TrancheStatus_TRANCHE_STATUS_STAKING
		contrib.Tranches[0].StakeDeadline = currentEpoch + params.StakeDeadlineEpochs
	}

	// Save updated contribution and new status index
	if err := k.Contribution.Set(ctx, contrib.Id, contrib); err != nil {
		return nil, err
	}
	if err := k.ContributionsByStatus.Set(ctx, collections.Join(int32(contrib.Status), contrib.Id)); err != nil {
		return nil, err
	}

	// Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"contribution_approved",
			sdk.NewAttribute("contribution_id", fmt.Sprintf("%d", contrib.Id)),
			sdk.NewAttribute("proposed_by", msg.Proposer),
		),
	)

	return &types.MsgApproveResponse{}, nil
}

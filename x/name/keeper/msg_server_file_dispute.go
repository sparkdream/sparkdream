package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/name/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func (k msgServer) FileDispute(goCtx context.Context, msg *types.MsgFileDispute) (*types.MsgFileDisputeResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)
	params := k.GetParams(ctx)

	// 1. Verify name exists and has an owner
	record, found := k.GetName(ctx, msg.Name)
	if !found {
		return nil, errorsmod.Wrapf(types.ErrNameNotFound, "name %q does not exist", msg.Name)
	}
	if record.Owner == "" {
		return nil, errorsmod.Wrapf(types.ErrNameNotFound, "name %q has no owner", msg.Name)
	}

	// 2. Check no active dispute already exists for this name
	existing, found := k.GetDispute(ctx, msg.Name)
	if found && existing.Active {
		return nil, errorsmod.Wrapf(types.ErrDisputeAlreadyExists, "name %q", msg.Name)
	}

	// 3. Lock claimant's DREAM stake
	stakeAmount := params.DisputeStakeDream
	if err := k.dreamOps.Lock(ctx, msg.Authority, stakeAmount.Uint64()); err != nil {
		return nil, errorsmod.Wrapf(types.ErrDREAMOperationFailed, "failed to lock DREAM stake: %s", err)
	}

	// 4. Create dispute record
	currentHeight := ctx.BlockHeight()
	dispute := types.Dispute{
		Name:        msg.Name,
		Claimant:    msg.Authority,
		FiledAt:     currentHeight,
		StakeAmount: stakeAmount,
		Active:      true,
	}
	if err := k.SetDispute(ctx, dispute); err != nil {
		return nil, err
	}

	// 5. Store DisputeStake record
	challengeID := fmt.Sprintf("name_dispute:%s:%d", msg.Name, currentHeight)
	disputeStake := types.DisputeStake{
		ChallengeId: challengeID,
		Staker:      msg.Authority,
		Amount:      stakeAmount,
	}
	if err := k.DisputeStakes.Set(ctx, challengeID, disputeStake); err != nil {
		return nil, err
	}

	// 6. Emit event
	ctx.EventManager().EmitEvent(
		sdk.NewEvent(
			"name_dispute_filed",
			sdk.NewAttribute("name", msg.Name),
			sdk.NewAttribute("claimant", msg.Authority),
			sdk.NewAttribute("stake_amount", stakeAmount.String()),
			sdk.NewAttribute("reason", msg.Reason),
			sdk.NewAttribute("filed_at", fmt.Sprintf("%d", currentHeight)),
		),
	)

	return &types.MsgFileDisputeResponse{}, nil
}

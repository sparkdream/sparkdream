package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/name/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func (k msgServer) ContestDispute(goCtx context.Context, msg *types.MsgContestDispute) (*types.MsgContestDisputeResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)
	params := k.GetParams(ctx)

	// 1. Verify active dispute exists
	dispute, found := k.GetDispute(ctx, msg.Name)
	if !found {
		return nil, errorsmod.Wrapf(types.ErrDisputeNotFound, "no dispute for name %q", msg.Name)
	}
	if !dispute.Active {
		return nil, errorsmod.Wrapf(types.ErrDisputeNotActive, "dispute for name %q is not active", msg.Name)
	}

	// 2. Verify not already contested
	if dispute.ContestChallengeId != "" {
		return nil, errorsmod.Wrapf(types.ErrContestAlreadyFiled, "dispute for name %q already contested", msg.Name)
	}

	// 3. Verify caller is the current name owner
	record, found := k.GetName(ctx, msg.Name)
	if !found || record.Owner != msg.Authority {
		return nil, errorsmod.Wrapf(types.ErrNotNameOwner, "only the owner of %q can contest", msg.Name)
	}

	// 4. Verify contest period hasn't expired
	currentHeight := ctx.BlockHeight()
	deadline := dispute.FiledAt + int64(params.DisputeTimeoutBlocks)
	if currentHeight > deadline {
		return nil, errorsmod.Wrapf(types.ErrContestPeriodExpired,
			"contest deadline was block %d, current block is %d", deadline, currentHeight)
	}

	// 5. Lock owner's DREAM stake
	contestStake := params.ContestStakeDream
	if err := k.dreamOps.Lock(ctx, msg.Authority, contestStake.Uint64()); err != nil {
		return nil, errorsmod.Wrapf(types.ErrDREAMOperationFailed, "failed to lock contest DREAM stake: %s", err)
	}

	// 6. Generate contest challenge ID and update dispute
	contestChallengeID := fmt.Sprintf("name_contest:%s:%d", msg.Name, currentHeight)
	dispute.ContestChallengeId = contestChallengeID
	dispute.ContestedAt = currentHeight
	if err := k.SetDispute(ctx, dispute); err != nil {
		return nil, err
	}

	// 7. Store ContestStake record
	contestStakeRecord := types.ContestStake{
		ChallengeId: contestChallengeID,
		Owner:       msg.Authority,
		Amount:      contestStake,
	}
	if err := k.ContestStakes.Set(ctx, contestChallengeID, contestStakeRecord); err != nil {
		return nil, err
	}

	// 8. Emit event (jury integration via x/rep happens off-chain or via event listeners)
	ctx.EventManager().EmitEvent(
		sdk.NewEvent(
			"name_dispute_contested",
			sdk.NewAttribute("name", msg.Name),
			sdk.NewAttribute("owner", msg.Authority),
			sdk.NewAttribute("contest_stake", contestStake.String()),
			sdk.NewAttribute("contest_challenge_id", contestChallengeID),
			sdk.NewAttribute("reason", msg.Reason),
			sdk.NewAttribute("contested_at", fmt.Sprintf("%d", currentHeight)),
		),
	)

	return &types.MsgContestDisputeResponse{}, nil
}

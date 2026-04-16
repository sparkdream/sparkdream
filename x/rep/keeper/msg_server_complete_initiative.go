package keeper

import (
	"context"

	"sparkdream/x/rep/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func (k msgServer) CompleteInitiative(ctx context.Context, msg *types.MsgCompleteInitiative) (*types.MsgCompleteInitiativeResponse, error) {
	creatorBytes, err := k.addressCodec.StringToBytes(msg.Creator)
	if err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}
	creatorAddr := sdk.AccAddress(creatorBytes)

	// Authorization check: caller must be the initiative's assignee or an operations committee member
	initiative, err := k.Keeper.GetInitiative(ctx, msg.InitiativeId)
	if err != nil {
		return nil, err
	}

	isAssignee := initiative.Assignee == msg.Creator
	isOpsCommittee := k.Keeper.IsOperationsCommittee(ctx, creatorAddr)
	if !isAssignee && !isOpsCommittee {
		return nil, errorsmod.Wrapf(types.ErrUnauthorized, "only the assignee or operations committee can complete an initiative")
	}

	// Complete the initiative (handles all rewards distribution)
	if err := k.Keeper.CompleteInitiative(ctx, msg.InitiativeId); err != nil {
		return nil, err
	}

	return &types.MsgCompleteInitiativeResponse{}, nil
}

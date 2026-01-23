package keeper

import (
	"context"

	"sparkdream/x/rep/types"

	errorsmod "cosmossdk.io/errors"
)

func (k msgServer) AssignInitiative(ctx context.Context, msg *types.MsgAssignInitiative) (*types.MsgAssignInitiativeResponse, error) {
	assigneeAddr, err := k.addressCodec.StringToBytes(msg.Assignee)
	if err != nil {
		return nil, errorsmod.Wrap(err, "invalid assignee address")
	}

	// Assign initiative to member
	err = k.Keeper.AssignInitiativeToMember(ctx, msg.InitiativeId, assigneeAddr)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to assign initiative")
	}

	return &types.MsgAssignInitiativeResponse{}, nil
}

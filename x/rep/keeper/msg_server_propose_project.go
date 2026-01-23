package keeper

import (
	"context"

	"sparkdream/x/rep/types"

	errorsmod "cosmossdk.io/errors"
)

func (k msgServer) ProposeProject(ctx context.Context, msg *types.MsgProposeProject) (*types.MsgProposeProjectResponse, error) {
	creatorAddr, err := k.addressCodec.StringToBytes(msg.Creator)
	if err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	// Validate creator is a member
	_, err = k.GetMember(ctx, creatorAddr)
	if err != nil {
		return nil, errorsmod.Wrap(types.ErrNotMember, "creator must be a member")
	}

	// Create project
	_, err = k.CreateProject(
		ctx,
		creatorAddr,
		msg.Name,
		msg.Description,
		msg.Tags,
		msg.Category,
		msg.Council,
		*msg.RequestedBudget,
		*msg.RequestedSpark,
	)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to create project")
	}

	return &types.MsgProposeProjectResponse{}, nil
}

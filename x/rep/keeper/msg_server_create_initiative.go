package keeper

import (
	"context"

	"sparkdream/x/rep/types"

	errorsmod "cosmossdk.io/errors"
)

func (k msgServer) CreateInitiative(ctx context.Context, msg *types.MsgCreateInitiative) (*types.MsgCreateInitiativeResponse, error) {
	creatorAddr, err := k.addressCodec.StringToBytes(msg.Creator)
	if err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	// Validate creator is a member
	_, err = k.GetMember(ctx, creatorAddr)
	if err != nil {
		return nil, errorsmod.Wrap(types.ErrNotMember, "creator must be a member")
	}

	// Create initiative using keeper method
	initiativeID, err := k.Keeper.CreateInitiative(
		ctx,
		creatorAddr,
		msg.ProjectId,
		msg.Title,
		msg.Description,
		msg.Tags,
		types.InitiativeTier(msg.Tier),
		types.InitiativeCategory(msg.Category),
		msg.TemplateId,
		*msg.Budget,
	)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to create initiative")
	}

	return &types.MsgCreateInitiativeResponse{
		InitiativeId: initiativeID,
	}, nil
}

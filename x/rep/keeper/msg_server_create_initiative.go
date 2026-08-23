package keeper

import (
	"context"

	"sparkdream/x/rep/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
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

	// Commissioning work against a budget-backed project is restricted to the
	// project's own creator, or the Operations Committee as an administrative
	// escape hatch (the same standing that lets it assign and cancel).
	//
	// Nothing used to check this. A project's approved_budget is a ceiling a
	// council voted for, and CreateInitiative draws against it via
	// AllocateBudget, which validates only that the project is ACTIVE and has
	// room left. Any member could therefore commission work against somebody
	// else's council-approved budget and exhaust it.
	//
	// Permissionless projects are deliberately exempt: open contribution is the
	// entire point of that mode, and it has its own gates (trust level, tier
	// cap, creation-fee burn, and — for self-assignment — the bond).
	project, err := k.Keeper.GetProject(ctx, msg.ProjectId)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to get parent project")
	}
	if !project.Permissionless && msg.Creator != project.Creator &&
		!k.Keeper.IsOperationsCommittee(ctx, sdk.AccAddress(creatorAddr)) {
		return nil, errorsmod.Wrapf(types.ErrUnauthorized,
			"only the creator of project %d or the operations committee may create initiatives under it",
			msg.ProjectId)
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
		*msg.Budget,
		msg.AcceptanceCriteria...,
	)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to create initiative")
	}

	return &types.MsgCreateInitiativeResponse{
		InitiativeId: initiativeID,
	}, nil
}

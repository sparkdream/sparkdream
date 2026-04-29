package keeper

import (
	"context"

	"sparkdream/x/rep/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func (k msgServer) AssignInitiative(ctx context.Context, msg *types.MsgAssignInitiative) (*types.MsgAssignInitiativeResponse, error) {
	assigneeAddr, err := k.addressCodec.StringToBytes(msg.Assignee)
	if err != nil {
		return nil, errorsmod.Wrap(err, "invalid assignee address")
	}

	creatorBytes, err := k.addressCodec.StringToBytes(msg.Creator)
	if err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	// Load initiative + parent project to check authorization.
	initiative, err := k.Keeper.GetInitiative(ctx, msg.InitiativeId)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to get initiative")
	}
	project, err := k.Keeper.GetProject(ctx, initiative.ProjectId)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to get parent project")
	}

	if msg.Creator != msg.Assignee && msg.Creator != project.Creator {
		isOps, opsErr := k.Keeper.commonsKeeper.IsCommitteeMember(ctx, sdk.AccAddress(creatorBytes), "commons", "operations")
		if opsErr != nil {
			return nil, errorsmod.Wrap(opsErr, "failed to check operations committee membership")
		}
		if !isOps {
			return nil, errorsmod.Wrap(types.ErrUnauthorized, "only the assignee, project creator, or operations committee may assign")
		}
	}

	// Assign initiative to member
	err = k.Keeper.AssignInitiativeToMember(ctx, msg.InitiativeId, assigneeAddr)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to assign initiative")
	}

	return &types.MsgAssignInitiativeResponse{}, nil
}

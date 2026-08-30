package keeper

import (
	"context"

	"sparkdream/x/rep/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func (k msgServer) CloseInitiative(ctx context.Context, msg *types.MsgCloseInitiative) (*types.MsgCloseInitiativeResponse, error) {
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

	if msg.Creator != project.Creator {
		isOps, opsErr := k.Keeper.commonsKeeper.IsCommitteeMember(ctx, sdk.AccAddress(creatorBytes), "commons", "operations")
		if opsErr != nil {
			return nil, errorsmod.Wrap(opsErr, "failed to check operations committee membership")
		}
		if !isOps {
			return nil, errorsmod.Wrap(types.ErrUnauthorized, "only the project creator or operations committee may close an initiative")
		}
	}

	if err := k.Keeper.CloseInitiative(ctx, msg.InitiativeId, msg.Reason); err != nil {
		return nil, errorsmod.Wrap(err, "failed to close initiative")
	}

	return &types.MsgCloseInitiativeResponse{}, nil
}

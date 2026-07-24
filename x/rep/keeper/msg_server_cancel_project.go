package keeper

import (
	"context"

	"sparkdream/x/rep/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func (k msgServer) CancelProject(ctx context.Context, msg *types.MsgCancelProject) (*types.MsgCancelProjectResponse, error) {
	creatorBytes, err := k.addressCodec.StringToBytes(msg.Creator)
	if err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	// Load the project to check authorization. Only the project creator or a
	// member of the project's council Operations Committee (the committee that
	// approves and funds it) may cancel — otherwise any address could retire
	// any project, including budget-backed ones with active initiatives.
	project, err := k.Keeper.GetProject(ctx, msg.ProjectId)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to get project")
	}

	if msg.Creator != project.Creator {
		isOps, opsErr := k.Keeper.commonsKeeper.IsCommitteeMember(ctx, sdk.AccAddress(creatorBytes), project.Council, "operations")
		if opsErr != nil {
			return nil, errorsmod.Wrap(opsErr, "failed to check operations committee membership")
		}
		if !isOps {
			return nil, errorsmod.Wrapf(types.ErrUnauthorized,
				"only the project creator or the Operations Committee for council '%s' may cancel a project", project.Council)
		}
	}

	// Cancel the project using the keeper method
	if err := k.Keeper.CancelProject(ctx, msg.ProjectId, msg.Reason); err != nil {
		return nil, errorsmod.Wrap(err, "failed to cancel project")
	}

	return &types.MsgCancelProjectResponse{}, nil
}

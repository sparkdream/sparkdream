package keeper

import (
	"context"

	"sparkdream/x/rep/types"

	errorsmod "cosmossdk.io/errors"
)

// CompleteProject records an ACTIVE project as COMPLETED (REP-E-21).
//
// Completion moves value — it mints the staker completion bonus and settles
// every project stake — so it is a decision of the body that approved the
// project, never of one of its members: the signer must be the gov authority
// or the policy address of the project's council or of that council's
// Operations Committee (an executed proposal). The keeper refuses while any
// initiative of the project is non-terminal or any challenge on one is
// active.
func (k msgServer) CompleteProject(ctx context.Context, msg *types.MsgCompleteProject) (*types.MsgCompleteProjectResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	project, err := k.Keeper.GetProject(ctx, msg.ProjectId)
	if err != nil {
		return nil, errorsmod.Wrap(types.ErrProjectNotFound, err.Error())
	}

	if !k.Keeper.isCouncilOrCommitteePolicy(ctx, msg.Creator, project.Council, "operations") {
		return nil, errorsmod.Wrapf(types.ErrUnauthorized,
			"only the policy of council '%s' or its Operations Committee (an executed proposal), or x/gov, may complete a project", project.Council)
	}

	if err := k.Keeper.CompleteProject(ctx, msg.ProjectId); err != nil {
		return nil, err
	}

	return &types.MsgCompleteProjectResponse{}, nil
}

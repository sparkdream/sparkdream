package keeper

import (
	"context"

	"sparkdream/x/rep/types"

	errorsmod "cosmossdk.io/errors"
)

// CancelProject retires a PROPOSED or ACTIVE project.
//
// Cancelling an approved project cascade-closes its initiatives (returning
// their budget, releasing self-assign bonds and voiding active challenges)
// and settles every project stake, so it is a decision of the body that
// approved the project, never of one of its members: the gov authority or
// the policy address of the project's council or of that council's
// Operations Committee (an executed proposal) may cancel any live project.
//
// The project creator keeps a narrower right of withdrawal:
//   - a PROPOSED project (nothing approved or funded yet, and no initiative
//     can exist under it), and
//   - an ACTIVE permissionless project (no council budget behind it) that has
//     no open initiative and no active challenge, so the creator can neither
//     close other members' work nor void a challenge on their own.
//
// An ACTIVE budget-backed project is never cancellable by its creator alone.
func (k msgServer) CancelProject(ctx context.Context, msg *types.MsgCancelProject) (*types.MsgCancelProjectResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	project, err := k.Keeper.GetProject(ctx, msg.ProjectId)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to get project")
	}

	if !k.Keeper.isCouncilOrCommitteePolicy(ctx, msg.Creator, project.Council, "operations") {
		if !k.Keeper.CanActAs(ctx, msg.Creator, project.Creator) {
			return nil, errorsmod.Wrapf(types.ErrUnauthorized,
				"only the project creator, the policy of council '%s' or its Operations Committee (an executed proposal), or x/gov may cancel a project", project.Council)
		}
		switch {
		case project.Status == types.ProjectStatus_PROJECT_STATUS_PROPOSED:
			// Withdrawing an unapproved proposal.
		case project.Status == types.ProjectStatus_PROJECT_STATUS_ACTIVE && project.Permissionless:
			if err := k.Keeper.requireNoOpenProjectWork(ctx, project.Id); err != nil {
				return nil, errorsmod.Wrap(err, "the creator may cancel an active permissionless project only once its work is closed")
			}
		case project.Status == types.ProjectStatus_PROJECT_STATUS_ACTIVE:
			return nil, errorsmod.Wrapf(types.ErrUnauthorized,
				"an approved budget-backed project can be cancelled only by the policy of council '%s' or its Operations Committee (an executed proposal), or x/gov", project.Council)
		}
		// Terminal statuses fall through to the keeper, which refuses them.
	}

	// Cancel the project using the keeper method
	if err := k.Keeper.CancelProject(ctx, msg.ProjectId, msg.Reason); err != nil {
		return nil, errorsmod.Wrap(err, "failed to cancel project")
	}

	return &types.MsgCancelProjectResponse{}, nil
}

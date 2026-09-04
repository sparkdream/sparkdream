package keeper

import (
	"context"

	"sparkdream/x/rep/types"

	errorsmod "cosmossdk.io/errors"
)

func (k msgServer) AssignInterim(ctx context.Context, msg *types.MsgAssignInterim) (*types.MsgAssignInterimResponse, error) {
	creatorAddr, err := k.addressCodec.StringToBytes(msg.Creator)
	if err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	assigneeAddr, err2 := k.addressCodec.StringToBytes(msg.Assignee)
	if err2 != nil {
		return nil, errorsmod.Wrap(err2, "invalid assignee address")
	}

	// Being an assignee entitles you to a share of the interim's budget, so
	// adding one is a spending decision. This had no authorization whatsoever:
	// anyone could add themselves to anyone's PENDING interim and collect on
	// completion. Restricted to the Operations Committee, the same authority
	// that approves interim payment in ApproveInterim.
	if !k.Keeper.IsOperationsCommittee(ctx, creatorAddr) {
		return nil, errorsmod.Wrapf(types.ErrUnauthorized,
			"only the Operations Committee may assign interim work (creator: %s)", msg.Creator)
	}

	// Assign the interim using the keeper method
	if err := k.Keeper.AssignInterimToMember(ctx, msg.InterimId, assigneeAddr); err != nil {
		return nil, errorsmod.Wrap(err, "failed to assign interim")
	}

	return &types.MsgAssignInterimResponse{}, nil
}

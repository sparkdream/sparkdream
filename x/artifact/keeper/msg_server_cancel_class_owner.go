package keeper

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/artifact/types"
)

func (k msgServer) CancelClassOwner(ctx context.Context, msg *types.MsgCancelClassOwner) (*types.MsgCancelClassOwnerResponse, error) {
	if _, err := k.decodeAddr("owner", msg.Owner); err != nil {
		return nil, err
	}
	c, err := k.getOwnedClass(ctx, msg.ClassId, msg.Owner)
	if err != nil {
		return nil, err
	}
	po, err := k.PendingClassOwners.Get(ctx, c.Id)
	if err != nil {
		return nil, types.ErrPendingOwnerNotFound
	}
	if err := k.clearPendingOwner(ctx, c.Id); err != nil {
		return nil, err
	}
	emit(ctx, types.EventClassOwnerCancelled, classAttr(c.Id),
		sdk.NewAttribute("from", c.Owner), sdk.NewAttribute("to", po.ProposedOwner))
	return &types.MsgCancelClassOwnerResponse{}, nil
}

package keeper

import (
	"context"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/artifact/types"
)

// clearPendingOwner deletes a class's handover proposal and its expiry entry.
func (k Keeper) clearPendingOwner(ctx context.Context, classID uint64) error {
	po, err := k.PendingClassOwners.Get(ctx, classID)
	if err != nil {
		return nil
	}
	if err := k.PendingOwnerExpiry.Remove(ctx, collections.Join(po.ExpiresAt, classID)); err != nil {
		return err
	}
	return k.PendingClassOwners.Remove(ctx, classID)
}

func (k msgServer) ProposeClassOwner(ctx context.Context, msg *types.MsgProposeClassOwner) (*types.MsgProposeClassOwnerResponse, error) {
	if _, err := k.decodeAddr("owner", msg.Owner); err != nil {
		return nil, err
	}
	c, err := k.getOwnedClass(ctx, msg.ClassId, msg.Owner)
	if err != nil {
		return nil, err
	}
	if msg.ProposedOwner == msg.Owner {
		return nil, errorsmod.Wrap(types.ErrInvalidRecipient, "proposed owner is the current owner")
	}
	if _, err := k.validateRecipient("proposed_owner", msg.ProposedOwner); err != nil {
		return nil, err
	}
	if err := k.clearPendingOwner(ctx, c.Id); err != nil {
		return nil, err
	}
	po := types.PendingClassOwner{
		ClassId:       c.Id,
		ProposedOwner: msg.ProposedOwner,
		ExpiresAt:     now(ctx) + k.GetParams(ctx).PendingTtl,
	}
	if err := k.PendingClassOwners.Set(ctx, c.Id, po); err != nil {
		return nil, err
	}
	if err := k.PendingOwnerExpiry.Set(ctx, collections.Join(po.ExpiresAt, c.Id)); err != nil {
		return nil, err
	}
	emit(ctx, types.EventClassOwnerProposed, classAttr(c.Id),
		sdk.NewAttribute("from", c.Owner), sdk.NewAttribute("to", msg.ProposedOwner),
		sdk.NewAttribute("expires_at", i64(po.ExpiresAt)))
	return &types.MsgProposeClassOwnerResponse{}, nil
}

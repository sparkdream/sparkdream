package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/collect/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// UnpinCollection clears the display-only `pinned` marker on a collection.
// The same daily counter as PinCollection / MakeCollectionPermanent applies
// so a Pin → Unpin → Pin rotation can't bypass the per-day cap.
func (k msgServer) UnpinCollection(ctx context.Context, msg *types.MsgUnpinCollection) (*types.MsgUnpinCollectionResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)

	coll, err := k.Collection.Get(ctx, msg.CollectionId)
	if err != nil {
		return nil, errorsmod.Wrap(types.ErrCollectionNotFound, fmt.Sprintf("collection %d not found", msg.CollectionId))
	}

	if !coll.Pinned {
		return nil, errorsmod.Wrap(types.ErrCollectionNotPinned, fmt.Sprintf("collection %d is not pinned", msg.CollectionId))
	}

	params, err := k.Params.Get(ctx)
	if err != nil {
		return nil, err
	}

	creatorAddr, _ := sdk.AccAddressFromBech32(msg.Creator)
	if k.repKeeper == nil {
		return nil, errorsmod.Wrap(types.ErrPinTrustLevelTooLow, "reputation module not available")
	}
	if !k.repKeeper.IsMember(ctx, creatorAddr) {
		return nil, errorsmod.Wrap(types.ErrPinTrustLevelTooLow, "not a member")
	}
	tl, err := k.repKeeper.GetTrustLevel(ctx, creatorAddr)
	if err != nil {
		return nil, errorsmod.Wrap(types.ErrPinTrustLevelTooLow, "cannot determine trust level")
	}
	if uint32(tl) < params.PinMinTrustLevel {
		return nil, errorsmod.Wrap(types.ErrPinTrustLevelTooLow, "does not meet pin trust level requirement")
	}

	if err := k.checkDailyLimit(ctx, msg.Creator, sdkCtx.BlockHeight(), "pin", params.MaxPinsPerDay); err != nil {
		return nil, err
	}

	coll.Pinned = false
	if err := k.Collection.Set(ctx, coll.Id, coll); err != nil {
		return nil, err
	}
	// Re-point the status index so its pinned-rank reflects the cleared marker.
	if err := k.MoveCollectionStatusIndex(ctx, coll.Status, true, coll.Status, false, coll.Id); err != nil {
		return nil, err
	}

	sdkCtx.EventManager().EmitEvent(sdk.NewEvent("collect.collection.unpinned",
		sdk.NewAttribute("collection_id", fmt.Sprintf("%d", msg.CollectionId)),
		sdk.NewAttribute("unpinned_by", msg.Creator),
	))

	return &types.MsgUnpinCollectionResponse{}, nil
}

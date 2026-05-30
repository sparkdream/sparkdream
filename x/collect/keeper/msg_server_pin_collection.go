package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/collect/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// PinCollection sets the display-only `pinned` marker on a permanent
// collection. Lifecycle promotion (ephemeral → permanent + deposit burn) is
// owned by MsgMakeCollectionPermanent — Pin refuses to accept an ephemeral
// target so callers can't conflate "feature this" with "preserve this".
//
// Gates:
//   - trust level ≥ params.pin_min_trust_level (default ESTABLISHED)
//   - target must already be permanent (expires_at == 0); ephemeral → ErrCannotPinEphemeral
//   - target must be ACTIVE (not HIDDEN / PENDING)
//   - daily pin counter (shared with MakeCollectionPermanent so a
//     Pin → Unpin → Pin → MakePermanent rotation can't bypass the per-day cap)
func (k msgServer) PinCollection(ctx context.Context, msg *types.MsgPinCollection) (*types.MsgPinCollectionResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)

	coll, err := k.Collection.Get(ctx, msg.CollectionId)
	if err != nil {
		return nil, errorsmod.Wrap(types.ErrCollectionNotFound, fmt.Sprintf("collection %d not found", msg.CollectionId))
	}

	if coll.Status != types.CollectionStatus_COLLECTION_STATUS_ACTIVE {
		return nil, errorsmod.Wrap(types.ErrCollectionNotFound, fmt.Sprintf("collection %d is not active", msg.CollectionId))
	}

	// Display-only Pin: requires the collection to already be permanent.
	// Lifecycle change belongs to MsgMakeCollectionPermanent.
	if coll.ExpiresAt > 0 {
		return nil, errorsmod.Wrap(types.ErrCannotPinEphemeral,
			fmt.Sprintf("collection %d is ephemeral; call MsgMakeCollectionPermanent first", msg.CollectionId))
	}

	if coll.Pinned {
		return nil, errorsmod.Wrap(types.ErrCollectionAlreadyPinned, fmt.Sprintf("collection %d is already pinned", msg.CollectionId))
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

	coll.Pinned = true
	if err := k.Collection.Set(ctx, coll.Id, coll); err != nil {
		return nil, err
	}

	sdkCtx.EventManager().EmitEvent(sdk.NewEvent("collect.collection.pinned",
		sdk.NewAttribute("collection_id", fmt.Sprintf("%d", msg.CollectionId)),
		sdk.NewAttribute("pinned_by", msg.Creator),
	))

	return &types.MsgPinCollectionResponse{}, nil
}

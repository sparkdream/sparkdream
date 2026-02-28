package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/collect/types"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func (k msgServer) PinCollection(ctx context.Context, msg *types.MsgPinCollection) (*types.MsgPinCollectionResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid authority address")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Get collection, must exist
	coll, err := k.Collection.Get(ctx, msg.CollectionId)
	if err != nil {
		return nil, errorsmod.Wrap(types.ErrCollectionNotFound, fmt.Sprintf("collection %d not found", msg.CollectionId))
	}

	// Must be active
	if coll.Status != types.CollectionStatus_COLLECTION_STATUS_ACTIVE {
		return nil, errorsmod.Wrap(types.ErrCollectionNotFound, fmt.Sprintf("collection %d is not active", msg.CollectionId))
	}

	// Must be ephemeral (has TTL)
	if coll.ExpiresAt == 0 {
		return nil, errorsmod.Wrap(types.ErrCannotPinActive, "collection is already permanent")
	}

	// Must not be expired
	if coll.ExpiresAt <= sdkCtx.BlockHeight() {
		return nil, errorsmod.Wrap(types.ErrCollectionExpired, fmt.Sprintf("collection %d has expired", msg.CollectionId))
	}

	params, err := k.Params.Get(ctx)
	if err != nil {
		return nil, err
	}

	// Creator must meet pin trust level
	creatorAddr, _ := sdk.AccAddressFromBech32(msg.Creator)
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

	// Rate limit check (reuse daily limit infrastructure with "pin" category)
	if err := k.checkDailyLimit(ctx, msg.Creator, sdkCtx.BlockHeight(), "pin", params.MaxPinsPerDay); err != nil {
		return nil, err
	}

	// Remove from expiry index
	k.CollectionsByExpiry.Remove(ctx, collections.Join(coll.ExpiresAt, coll.Id)) //nolint:errcheck

	// Update collection: make permanent
	coll.ExpiresAt = 0
	coll.DepositBurned = true
	if coll.ConvictionSustained {
		coll.ConvictionSustained = false
	}

	// Burn held deposits (collection + item) from module account
	totalDeposit := coll.DepositAmount.Add(coll.ItemDepositTotal)
	if totalDeposit.IsPositive() {
		if err := k.BurnSPARK(ctx, totalDeposit); err != nil {
			return nil, err
		}
	}

	if err := k.Collection.Set(ctx, coll.Id, coll); err != nil {
		return nil, err
	}

	// Emit event
	sdkCtx.EventManager().EmitEvent(sdk.NewEvent("collect.collection.pinned",
		sdk.NewAttribute("collection_id", fmt.Sprintf("%d", msg.CollectionId)),
		sdk.NewAttribute("pinned_by", msg.Creator),
	))

	return &types.MsgPinCollectionResponse{}, nil
}

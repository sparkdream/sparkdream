package keeper

import (
	"context"
	"strconv"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/collect/types"
)

func (k msgServer) SponsorCollection(ctx context.Context, msg *types.MsgSponsorCollection) (*types.MsgSponsorCollectionResponse, error) {
	sponsorAddr, err := k.addressCodec.StringToBytes(msg.Creator)
	if err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	params, err := k.Params.Get(ctx)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to get params")
	}

	// Creator must be x/rep member at min_sponsor_trust_level
	if !k.meetsMinTrustLevel(ctx, msg.Creator, params.MinSponsorTrustLevel) {
		return nil, errorsmod.Wrapf(types.ErrSponsorTrustLevelTooLow, "must be at or above %s", params.MinSponsorTrustLevel)
	}

	// Get collection
	coll, err := k.Collection.Get(ctx, msg.CollectionId)
	if err != nil {
		return nil, errorsmod.Wrap(types.ErrCollectionNotFound, "collection not found")
	}

	// Creator must NOT be collection owner
	if coll.Owner == msg.Creator {
		return nil, errorsmod.Wrap(types.ErrCannotSponsorOwn, "sponsor cannot be the collection owner")
	}

	// Active sponsorship request must exist
	req, err := k.SponsorshipRequest.Get(ctx, msg.CollectionId)
	if err != nil {
		return nil, errorsmod.Wrap(types.ErrSponsorshipRequestNotFound, "no pending sponsorship request")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	currentBlock := sdkCtx.BlockHeight()

	// Collection must still be TTL (not expired)
	if coll.ExpiresAt > 0 && coll.ExpiresAt <= currentBlock {
		return nil, errorsmod.Wrap(types.ErrCollectionExpired, "collection has already expired")
	}
	if coll.ExpiresAt == 0 {
		return nil, errorsmod.Wrap(types.ErrCollectionAlreadyPermanent, "collection is already permanent")
	}

	// Charge sponsor_fee from creator (burned)
	if err := k.BurnSPARKFromAccount(ctx, sponsorAddr, params.SponsorFee); err != nil {
		return nil, errorsmod.Wrap(err, "failed to charge sponsor fee")
	}

	// Burn escrowed permanent deposits from module account
	escrowedTotal := req.CollectionDeposit.Add(req.ItemDepositTotal)
	if err := k.BurnSPARK(ctx, escrowedTotal); err != nil {
		return nil, errorsmod.Wrap(err, "failed to burn escrowed deposits")
	}

	// Refund original TTL deposits (deposit_amount + item_deposit_total on Collection) to owner
	ownerAddr, err := k.addressCodec.StringToBytes(coll.Owner)
	if err != nil {
		return nil, errorsmod.Wrap(err, "invalid owner address")
	}
	originalDeposit := coll.DepositAmount.Add(coll.ItemDepositTotal)
	if originalDeposit.IsPositive() {
		if err := k.RefundSPARK(ctx, ownerAddr, originalDeposit); err != nil {
			return nil, errorsmod.Wrap(err, "failed to refund original TTL deposits to owner")
		}
	}

	// Save old expiry before modifying (needed for index cleanup)
	oldExpiresAt := coll.ExpiresAt

	// Update collection fields
	coll.DepositAmount = req.CollectionDeposit
	coll.ItemDepositTotal = req.ItemDepositTotal
	coll.ExpiresAt = 0
	coll.DepositBurned = true
	coll.SponsoredBy = msg.Creator

	if err := k.Collection.Set(ctx, msg.CollectionId, coll); err != nil {
		return nil, errorsmod.Wrap(err, "failed to update collection")
	}

	// Delete SponsorshipRequest and indexes
	if err := k.SponsorshipRequestsByExpiry.Remove(ctx, collections.Join(req.ExpiresAt, msg.CollectionId)); err != nil {
		return nil, errorsmod.Wrap(err, "failed to remove sponsorship expiry index")
	}
	if err := k.SponsorshipRequest.Remove(ctx, msg.CollectionId); err != nil {
		return nil, errorsmod.Wrap(err, "failed to remove sponsorship request")
	}

	// Remove from CollectionsByExpiry (was TTL, now permanent)
	if oldExpiresAt > 0 {
		k.CollectionsByExpiry.Remove(ctx, collections.Join(oldExpiresAt, msg.CollectionId)) //nolint:errcheck
	}

	// Emit event
	sdkCtx.EventManager().EmitEvent(sdk.NewEvent("collection_sponsored",
		sdk.NewAttribute("collection_id", strconv.FormatUint(msg.CollectionId, 10)),
		sdk.NewAttribute("sponsor", msg.Creator),
		sdk.NewAttribute("owner", coll.Owner),
		sdk.NewAttribute("sponsor_fee", params.SponsorFee.String()),
		sdk.NewAttribute("deposits_burned", escrowedTotal.String()),
		sdk.NewAttribute("owner_refunded", originalDeposit.String()),
	))

	return &types.MsgSponsorCollectionResponse{}, nil
}

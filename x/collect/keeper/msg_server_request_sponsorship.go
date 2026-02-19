package keeper

import (
	"context"
	"strconv"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/collect/types"
)

func (k msgServer) RequestSponsorship(ctx context.Context, msg *types.MsgRequestSponsorship) (*types.MsgRequestSponsorshipResponse, error) {
	creatorAddr, err := k.addressCodec.StringToBytes(msg.Creator)
	if err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	params, err := k.Params.Get(ctx)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to get params")
	}

	// Creator must be collection owner
	coll, err := k.Collection.Get(ctx, msg.CollectionId)
	if err != nil {
		return nil, errorsmod.Wrap(types.ErrCollectionNotFound, "collection not found")
	}
	if coll.Owner != msg.Creator {
		return nil, errorsmod.Wrap(types.ErrUnauthorized, "only collection owner can request sponsorship")
	}

	// Collection must be TTL (expires_at > 0, deposit_burned=false)
	if coll.ExpiresAt == 0 {
		return nil, errorsmod.Wrap(types.ErrCollectionAlreadyPermanent, "collection is already permanent")
	}
	if coll.DepositBurned {
		return nil, errorsmod.Wrap(types.ErrAlreadySponsored, "collection deposit already burned (already sponsored)")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	currentBlock := sdkCtx.BlockHeight()

	// Collection must not be expired
	if coll.ExpiresAt <= currentBlock {
		return nil, errorsmod.Wrap(types.ErrCollectionExpired, "collection has already expired")
	}

	// Creator must NOT be x/rep member
	if k.isMember(ctx, msg.Creator) {
		return nil, errorsmod.Wrap(types.ErrMemberCannotRequestSponsorship, "members can convert to permanent directly")
	}

	// No existing sponsorship request
	_, err = k.SponsorshipRequest.Get(ctx, msg.CollectionId)
	if err == nil {
		return nil, errorsmod.Wrap(types.ErrSponsorshipRequestExists, "sponsorship request already exists")
	}

	// Collection not already sponsored
	if coll.SponsoredBy != "" {
		return nil, errorsmod.Wrap(types.ErrAlreadySponsored, "collection has already been sponsored")
	}

	// Calculate permanent deposit: base_collection_deposit + (item_count * per_item_deposit)
	collectionDeposit := params.BaseCollectionDeposit
	itemDepositTotal := params.PerItemDeposit.MulRaw(int64(coll.ItemCount))

	// Escrow permanent deposit from creator to module account
	totalEscrow := collectionDeposit.Add(itemDepositTotal)
	if err := k.EscrowSPARK(ctx, creatorAddr, totalEscrow); err != nil {
		return nil, errorsmod.Wrap(err, "failed to escrow permanent deposit")
	}

	// Create SponsorshipRequest
	expiresAt := currentBlock + params.SponsorshipRequestTtlBlocks
	req := types.SponsorshipRequest{
		CollectionId:      msg.CollectionId,
		Requester:         msg.Creator,
		CollectionDeposit: collectionDeposit,
		ItemDepositTotal:  itemDepositTotal,
		RequestedAt:       currentBlock,
		ExpiresAt:         expiresAt,
	}
	if err := k.SponsorshipRequest.Set(ctx, msg.CollectionId, req); err != nil {
		return nil, errorsmod.Wrap(err, "failed to store sponsorship request")
	}

	// Set SponsorshipRequestsByExpiry index
	if err := k.SponsorshipRequestsByExpiry.Set(ctx, collections.Join(expiresAt, msg.CollectionId)); err != nil {
		return nil, errorsmod.Wrap(err, "failed to set expiry index")
	}

	// Emit event
	sdkCtx.EventManager().EmitEvent(sdk.NewEvent("sponsorship_requested",
		sdk.NewAttribute("collection_id", strconv.FormatUint(msg.CollectionId, 10)),
		sdk.NewAttribute("requester", msg.Creator),
		sdk.NewAttribute("collection_deposit", collectionDeposit.String()),
		sdk.NewAttribute("item_deposit_total", itemDepositTotal.String()),
		sdk.NewAttribute("expires_at", strconv.FormatInt(expiresAt, 10)),
	))

	return &types.MsgRequestSponsorshipResponse{}, nil
}

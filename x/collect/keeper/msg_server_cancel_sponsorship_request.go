package keeper

import (
	"context"
	"strconv"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/collect/types"
)

func (k msgServer) CancelSponsorshipRequest(ctx context.Context, msg *types.MsgCancelSponsorshipRequest) (*types.MsgCancelSponsorshipRequestResponse, error) {
	creatorAddr, err := k.addressCodec.StringToBytes(msg.Creator)
	if err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	// Creator must be collection owner
	coll, err := k.Collection.Get(ctx, msg.CollectionId)
	if err != nil {
		return nil, errorsmod.Wrap(types.ErrCollectionNotFound, "collection not found")
	}
	if coll.Owner != msg.Creator {
		return nil, errorsmod.Wrap(types.ErrUnauthorized, "only collection owner can cancel sponsorship request")
	}

	// Active sponsorship request must exist
	req, err := k.SponsorshipRequest.Get(ctx, msg.CollectionId)
	if err != nil {
		return nil, errorsmod.Wrap(types.ErrSponsorshipRequestNotFound, "no pending sponsorship request")
	}

	// Refund escrowed collection_deposit + item_deposit_total from module to creator
	refundAmt := req.CollectionDeposit.Add(req.ItemDepositTotal)
	if refundAmt.IsPositive() {
		if err := k.RefundSPARK(ctx, creatorAddr, refundAmt); err != nil {
			return nil, errorsmod.Wrap(err, "failed to refund escrowed deposit")
		}
	}

	// Delete SponsorshipRequest and expiry index
	if err := k.SponsorshipRequestsByExpiry.Remove(ctx, collections.Join(req.ExpiresAt, msg.CollectionId)); err != nil {
		return nil, errorsmod.Wrap(err, "failed to remove expiry index")
	}
	if err := k.SponsorshipRequest.Remove(ctx, msg.CollectionId); err != nil {
		return nil, errorsmod.Wrap(err, "failed to remove sponsorship request")
	}

	// Emit event
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx.EventManager().EmitEvent(sdk.NewEvent("sponsorship_request_cancelled",
		sdk.NewAttribute("collection_id", strconv.FormatUint(msg.CollectionId, 10)),
		sdk.NewAttribute("requester", msg.Creator),
		sdk.NewAttribute("refunded", refundAmt.String()),
	))

	return &types.MsgCancelSponsorshipRequestResponse{}, nil
}

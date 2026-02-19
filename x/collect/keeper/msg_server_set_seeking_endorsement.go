package keeper

import (
	"context"
	"strconv"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/collect/types"
)

func (k msgServer) SetSeekingEndorsement(ctx context.Context, msg *types.MsgSetSeekingEndorsement) (*types.MsgSetSeekingEndorsementResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Get collection
	coll, err := k.Collection.Get(ctx, msg.CollectionId)
	if err != nil {
		return nil, types.ErrCollectionNotFound
	}

	// Creator must be collection owner
	if coll.Owner != msg.Creator {
		return nil, types.ErrUnauthorized
	}

	// Collection must have status PENDING
	if coll.Status != types.CollectionStatus_COLLECTION_STATUS_PENDING {
		return nil, types.ErrCollectionNotPending
	}

	// Collection must not already be endorsed
	if coll.EndorsedBy != "" {
		return nil, types.ErrAlreadyEndorsed
	}

	// Set seeking_endorsement
	coll.SeekingEndorsement = msg.Seeking
	if err := k.Collection.Set(ctx, coll.Id, coll); err != nil {
		return nil, errorsmod.Wrap(err, "failed to update collection")
	}

	// Emit event
	sdkCtx.EventManager().EmitEvent(sdk.NewEvent("seeking_endorsement_updated",
		sdk.NewAttribute("collection_id", strconv.FormatUint(coll.Id, 10)),
		sdk.NewAttribute("owner", msg.Creator),
		sdk.NewAttribute("seeking", strconv.FormatBool(msg.Seeking)),
	))

	return &types.MsgSetSeekingEndorsementResponse{}, nil
}

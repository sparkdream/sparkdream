package keeper

import (
	"bytes"
	"context"
	"crypto/sha256"

	"sparkdream/x/vote/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func (k msgServer) StoreSRS(ctx context.Context, msg *types.MsgStoreSRS) (*types.MsgStoreSRSResponse, error) {
	authorityAddr, err := k.addressCodec.StringToBytes(msg.Authority)
	if err != nil {
		return nil, errorsmod.Wrap(err, "invalid authority address")
	}

	// Must be governance module account.
	if !bytes.Equal(authorityAddr, k.authority) {
		return nil, errorsmod.Wrapf(types.ErrCancelNotAuthorized, "only governance module can store SRS")
	}

	params, err := k.Params.Get(ctx)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to get params")
	}

	// Compute SHA-256 of SRS bytes and compare to params.srs_hash.
	hash := sha256.Sum256(msg.Srs)
	if len(params.SrsHash) > 0 && !bytes.Equal(hash[:], params.SrsHash) {
		return nil, types.ErrSRSHashMismatch
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)

	srsState := types.SrsState{
		Srs:      msg.Srs,
		Hash:     hash[:],
		StoredAt: sdkCtx.BlockHeight(),
	}
	if err := k.SrsState.Set(ctx, srsState); err != nil {
		return nil, errorsmod.Wrap(err, "failed to store SRS")
	}

	sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventSRSStored,
	))

	return &types.MsgStoreSRSResponse{}, nil
}

package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/vote/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func (k msgServer) SubmitDecryptionShare(ctx context.Context, msg *types.MsgSubmitDecryptionShare) (*types.MsgSubmitDecryptionShareResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Validator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid validator address")
	}

	params, err := k.Params.Get(ctx)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to get params")
	}

	// Check TLE is enabled.
	if !params.TleEnabled {
		return nil, types.ErrTLENotEnabled
	}

	// Check validator has a registered share.
	valShare, err := k.TleValidatorShare.Get(ctx, msg.Validator)
	if err != nil {
		return nil, types.ErrNoTLEShare
	}

	// Check not already submitted for this epoch.
	shareKey := tleShareKey(msg.Validator, msg.Epoch)
	has, err := k.TleDecryptionShare.Has(ctx, shareKey)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to check decryption share")
	}
	if has {
		return nil, types.ErrDuplicateDecryptionShare
	}

	// Verify the submitted scalar matches the registered public key share.
	if err := verifyCorrectnessProof(ctx, msg.DecryptionShare, msg.CorrectnessProof, valShare.PublicKeyShare); err != nil {
		return nil, types.ErrInvalidCorrectnessProof
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Store decryption share.
	share := types.TleDecryptionShare{
		Index:       shareKey,
		Validator:   msg.Validator,
		Epoch:       msg.Epoch,
		Share:       msg.DecryptionShare,
		SubmittedAt: sdkCtx.BlockHeight(),
	}
	if err := k.TleDecryptionShare.Set(ctx, shareKey, share); err != nil {
		return nil, errorsmod.Wrap(err, "failed to store decryption share")
	}

	sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventDecryptionShareSubmit,
		sdk.NewAttribute(types.AttributeValidator, msg.Validator),
		sdk.NewAttribute(types.AttributeEpoch, fmt.Sprintf("%d", msg.Epoch)),
	))

	// Attempt epoch key reconstruction if threshold is met.
	if err := tryReconstructEpochKey(ctx, k.Keeper, msg.Epoch); err != nil {
		// Log but don't fail — the share was stored successfully.
		sdkCtx.Logger().Error("epoch key reconstruction failed", "epoch", msg.Epoch, "error", err)
	}

	return &types.MsgSubmitDecryptionShareResponse{}, nil
}

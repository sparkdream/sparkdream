package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/vote/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func (k msgServer) RegisterTLEShare(ctx context.Context, msg *types.MsgRegisterTLEShare) (*types.MsgRegisterTLEShareResponse, error) {
	addrBytes, err := k.addressCodec.StringToBytes(msg.Validator)
	if err != nil {
		return nil, errorsmod.Wrap(err, "invalid validator address")
	}

	// Guard 1: Verify sender is a bonded validator.
	valAddr := sdk.ValAddress(addrBytes)
	validator, err := k.stakingKeeper.GetValidator(ctx, valAddr)
	if err != nil {
		return nil, errorsmod.Wrap(types.ErrNotValidator, "validator not found in staking module")
	}
	if !validator.IsBonded() {
		return nil, errorsmod.Wrap(types.ErrNotValidator, "validator is not in bonded status")
	}

	// Guard 2: Validate share index is positive (1-based).
	if msg.ShareIndex == 0 {
		return nil, types.ErrInvalidShareIndex
	}

	// Guard 3: Validate public key share is a valid BN256 G1 point.
	point := tleSuite.Point()
	if err := point.UnmarshalBinary(msg.PublicKeyShare); err != nil {
		return nil, errorsmod.Wrap(types.ErrInvalidPublicKeyShare, err.Error())
	}

	// Guard 4: Ensure no other validator has the same share index.
	err = k.TleValidatorShare.Walk(ctx, nil, func(key string, vs types.TleValidatorShare) (bool, error) {
		if vs.ShareIndex == msg.ShareIndex && key != msg.Validator {
			return true, types.ErrDuplicateShareIndex
		}
		return false, nil
	})
	if err != nil {
		return nil, err
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Store/replace the TLE validator share.
	share := types.TleValidatorShare{
		Validator:      msg.Validator,
		PublicKeyShare: msg.PublicKeyShare,
		ShareIndex:     msg.ShareIndex,
		RegisteredAt:   sdkCtx.BlockHeight(),
	}
	if err := k.TleValidatorShare.Set(ctx, msg.Validator, share); err != nil {
		return nil, errorsmod.Wrap(err, "failed to store TLE share")
	}

	sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTLEShareRegistered,
		sdk.NewAttribute(types.AttributeValidator, msg.Validator),
		sdk.NewAttribute(types.AttributeShareIndex, fmt.Sprintf("%d", msg.ShareIndex)),
	))

	return &types.MsgRegisterTLEShareResponse{}, nil
}

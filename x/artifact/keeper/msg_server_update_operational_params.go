package keeper

import (
	"context"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/artifact/types"
)

// UpdateOperationalParams replaces the operational subset. Authorized for
// gov, the Commons Council policy, the Operations Committee policy, or any
// Operations Committee member; every field is clamped by the hard bounds.
func (k msgServer) UpdateOperationalParams(ctx context.Context, msg *types.MsgUpdateOperationalParams) (*types.MsgUpdateOperationalParamsResponse, error) {
	if _, err := k.decodeAddr("authority", msg.Authority); err != nil {
		return nil, err
	}
	if !k.isOpsAuthority(ctx, msg.Authority) {
		return nil, errorsmod.Wrapf(types.ErrInvalidSigner, "%s may not update operational params", msg.Authority)
	}
	if err := msg.OperationalParams.Validate(); err != nil {
		return nil, errorsmod.Wrap(types.ErrInvalidParams, err.Error())
	}
	merged := k.GetParams(ctx).ApplyOperationalParams(msg.OperationalParams)
	if err := merged.Validate(); err != nil {
		return nil, errorsmod.Wrap(types.ErrInvalidParams, err.Error())
	}
	if err := k.Params.Set(ctx, merged); err != nil {
		return nil, err
	}
	emit(ctx, types.EventOperationalParamsSet, sdk.NewAttribute("authority", msg.Authority))
	return &types.MsgUpdateOperationalParamsResponse{}, nil
}

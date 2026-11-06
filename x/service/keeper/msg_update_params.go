package keeper

import (
	"bytes"
	"context"

	errorsmod "cosmossdk.io/errors"

	"sparkdream/x/service/types"
)

func (k msgServer) UpdateParams(ctx context.Context, req *types.MsgUpdateParams) (*types.MsgUpdateParamsResponse, error) {
	authority, err := k.addressCodec.StringToBytes(req.Authority)
	if err != nil {
		return nil, errorsmod.Wrap(err, "invalid authority address")
	}

	if !bytes.Equal(k.GetAuthority(), authority) {
		expectedAuthorityStr, _ := k.addressCodec.BytesToString(k.GetAuthority())
		return nil, errorsmod.Wrapf(types.ErrInvalidSigner, "invalid authority; expected %s, got %s", expectedAuthorityStr, req.Authority)
	}

	if err := req.Params.Validate(); err != nil {
		return nil, err
	}

	// SVC-203: raising report_contest_window_blocks must not strand an
	// existing service type below it (its unbonding period is only
	// checked against the window when the type itself is written).
	if err := k.ServiceTypes.Walk(ctx, nil, func(_ string, cfg types.ServiceTypeConfig) (bool, error) {
		return false, cfg.ValidateAgainstParams(req.Params)
	}); err != nil {
		return nil, err
	}

	if err := k.Params.Set(ctx, req.Params); err != nil {
		return nil, err
	}

	return &types.MsgUpdateParamsResponse{}, nil
}

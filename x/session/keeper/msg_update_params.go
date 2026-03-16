package keeper

import (
	"bytes"
	"context"

	errorsmod "cosmossdk.io/errors"

	"sparkdream/x/session/types"
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

	// Basic validation
	if err := req.Params.Validate(); err != nil {
		return nil, err
	}

	// Ceiling enforcement: new ceiling must be subset of current ceiling.
	// Governance can shrink the ceiling but cannot expand it.
	currentParams, err := k.Params.Get(ctx)
	if err != nil {
		return nil, err
	}

	currentCeiling := make(map[string]bool, len(currentParams.MaxAllowedMsgTypes))
	for _, t := range currentParams.MaxAllowedMsgTypes {
		currentCeiling[t] = true
	}

	for _, t := range req.Params.MaxAllowedMsgTypes {
		if !currentCeiling[t] {
			return nil, types.ErrCeilingExpansion.Wrapf("type %s is not in current ceiling", t)
		}
	}

	if err := k.Params.Set(ctx, req.Params); err != nil {
		return nil, err
	}

	return &types.MsgUpdateParamsResponse{}, nil
}

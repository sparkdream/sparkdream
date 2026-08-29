package keeper

import (
	"bytes"
	"context"

	errorsmod "cosmossdk.io/errors"

	"sparkdream/x/rep/types"
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

	if err := k.Params.Set(ctx, req.Params); err != nil {
		return nil, err
	}

	// The reviewer bond floor and its exit terms live in params but are enforced
	// from the BondedRoleConfig, so a write that stopped here would change the
	// advertised policy without changing what BondRole actually checks.
	if err := k.SyncReviewerBondedRoleConfig(ctx, req.Params); err != nil {
		return nil, errorsmod.Wrap(err, "failed to sync reviewer bonded role config")
	}

	return &types.MsgUpdateParamsResponse{}, nil
}

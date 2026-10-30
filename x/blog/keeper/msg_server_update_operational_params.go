package keeper

import (
	"context"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/blog/types"
)

func (k msgServer) UpdateOperationalParams(ctx context.Context, req *types.MsgUpdateOperationalParams) (*types.MsgUpdateOperationalParamsResponse, error) {
	if !k.isCouncilAuthorized(ctx, req.Authority, "commons", "operations") {
		return nil, errorsmod.Wrapf(types.ErrInvalidSigner, "address %s is not authorized to update operational params", req.Authority)
	}

	if err := req.OperationalParams.Validate(); err != nil {
		return nil, errorsmod.Wrap(err, "invalid operational params")
	}

	currentParams, err := k.Params.Get(ctx)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to get current params")
	}

	merged := currentParams.ApplyOperationalParams(req.OperationalParams)

	if err := merged.Validate(); err != nil {
		return nil, errorsmod.Wrap(err, "merged params validation failed")
	}

	if err := k.Params.Set(ctx, merged); err != nil {
		return nil, errorsmod.Wrap(err, "failed to set params")
	}

	// Indexers and monitoring see every ops-committee change to fees, rate
	// limits and TTLs.
	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent(
		"blog.operational_params.updated",
		sdk.NewAttribute("authority", req.Authority),
		sdk.NewAttribute("operational_params", string(k.cdc.MustMarshalJSON(&req.OperationalParams))),
	))

	return &types.MsgUpdateOperationalParamsResponse{}, nil
}

package keeper

import (
	"context"

	errorsmod "cosmossdk.io/errors"

	"sparkdream/x/name/types"
)

func (k msgServer) UpdateOperationalParams(ctx context.Context, req *types.MsgUpdateOperationalParams) (*types.MsgUpdateOperationalParamsResponse, error) {
	// An executed Commons Council or Operations Committee proposal (or
	// x/gov); a single committee member may not move these clocks
	// (NAM-01). Values are also held inside compiled bounds by Validate.
	if !k.isCouncilOrCommitteePolicy(ctx, req.Authority, "commons", "operations") {
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

	return &types.MsgUpdateOperationalParamsResponse{}, nil
}

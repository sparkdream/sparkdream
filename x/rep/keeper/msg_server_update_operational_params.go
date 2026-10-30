package keeper

import (
	"context"

	errorsmod "cosmossdk.io/errors"

	"sparkdream/x/rep/types"
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

	// One Operations Committee member signs this message, so the epoch
	// clocks are out of its reach (REP-E-08): epoch_blocks is fixed at
	// genesis (see UpdateParams) and season_duration_epochs is governance
	// only. Resending the current values, as a full operational-params
	// update does, is accepted.
	if req.OperationalParams.EpochBlocks != currentParams.EpochBlocks {
		return nil, errorsmod.Wrapf(types.ErrParamImmutable, "epoch_blocks is %d", currentParams.EpochBlocks)
	}
	if req.OperationalParams.SeasonDurationEpochs != currentParams.SeasonDurationEpochs {
		return nil, errorsmod.Wrapf(types.ErrParamGovernanceOnly, "season_duration_epochs is %d", currentParams.SeasonDurationEpochs)
	}

	merged := currentParams.ApplyOperationalParams(req.OperationalParams)

	if err := merged.Validate(); err != nil {
		return nil, errorsmod.Wrap(err, "merged params validation failed")
	}

	if err := k.Params.Set(ctx, merged); err != nil {
		return nil, errorsmod.Wrap(err, "failed to set params")
	}

	// The reviewer bond floor and its exit terms live in params but are enforced
	// from the BondedRoleConfig, so a merge that stopped here would change the
	// advertised policy without changing what BondRole actually checks.
	if err := k.SyncReviewerBondedRoleConfig(ctx, merged); err != nil {
		return nil, errorsmod.Wrap(err, "failed to sync reviewer bonded role config")
	}

	return &types.MsgUpdateOperationalParamsResponse{}, nil
}

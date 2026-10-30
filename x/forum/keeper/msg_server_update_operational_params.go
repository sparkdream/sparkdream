package keeper

import (
	"context"

	errorsmod "cosmossdk.io/errors"

	"sparkdream/x/forum/types"
)

func (k msgServer) UpdateOperationalParams(ctx context.Context, req *types.MsgUpdateOperationalParams) (*types.MsgUpdateOperationalParamsResponse, error) {
	// Operational params move value (fees, taxes, bonds, rewards) and set
	// clocks (TTLs, appeal and archive windows), so they need an executed
	// Operations Committee decision (or governance), never one member.
	if !k.isCouncilOrCommitteePolicy(ctx, req.Authority, "commons", "operations") {
		return nil, errorsmod.Wrapf(types.ErrInvalidSigner, "address %s is not authorized to update operational params", req.Authority)
	}

	if err := req.OperationalParams.Validate(); err != nil {
		return nil, errorsmod.Wrap(err, "invalid operational params")
	}
	if err := req.OperationalParams.ValidateCommitteeBounds(); err != nil {
		return nil, errorsmod.Wrap(err, "operational params outside the committee bounds")
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

	// Write-through the sentinel bond-role config to x/rep so MsgBondRole
	// enforcement uses the same thresholds the council just set.
	if err := k.SyncSentinelBondedRoleConfig(ctx, merged); err != nil {
		return nil, errorsmod.Wrap(err, "failed to sync sentinel bonded-role config to rep")
	}

	return &types.MsgUpdateOperationalParamsResponse{}, nil
}

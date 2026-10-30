package keeper

import (
	"bytes"
	"context"

	errorsmod "cosmossdk.io/errors"

	"sparkdream/x/collect/types"
)

func (k msgServer) UpdateOperationalParams(ctx context.Context, msg *types.MsgUpdateOperationalParams) (*types.MsgUpdateOperationalParamsResponse, error) {
	// Operational params move value (fees, deposits, commits) and set clocks
	// (hide expiry, endorsement deadline), so a single committee member must
	// not change them: only the gov authority, the Commons Council policy or
	// the Commons Operations Committee policy (an executed proposal) may.
	if _, err := k.addressCodec.StringToBytes(msg.Authority); err != nil {
		return nil, errorsmod.Wrap(err, "invalid authority address")
	}

	authorized := false
	if k.commonsKeeper != nil {
		authorized = k.commonsKeeper.IsCouncilOrCommitteePolicy(ctx, msg.Authority, "commons", "operations")
	} else {
		// Fall back to governance authority check
		authorityAddr, _ := k.addressCodec.StringToBytes(msg.Authority)
		authorized = bytes.Equal(k.authority, authorityAddr)
	}

	if !authorized {
		return nil, errorsmod.Wrapf(types.ErrInvalidSigner, "address %s is not authorized to update operational params", msg.Authority)
	}

	// Validate operational params
	if err := msg.OperationalParams.Validate(); err != nil {
		return nil, errorsmod.Wrap(err, "invalid operational params")
	}

	// Fetch current params
	currentParams, err := k.Params.Get(ctx)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to get current params")
	}

	// Merge operational fields onto current params
	merged := currentParams.ApplyOperationalParams(msg.OperationalParams)

	// Validate merged params
	if err := merged.Validate(); err != nil {
		return nil, errorsmod.Wrap(err, "merged params validation failed")
	}
	if err := merged.ValidateOperationalBounds(); err != nil {
		return nil, errorsmod.Wrap(err, "operational hard bounds")
	}

	// Store merged params
	if err := k.Params.Set(ctx, merged); err != nil {
		return nil, errorsmod.Wrap(err, "failed to set params")
	}

	// Write-through the curator bond-role config to x/rep so MsgBondRole
	// enforcement uses the same thresholds the council just set.
	if err := k.SyncCuratorBondedRoleConfig(ctx, merged); err != nil {
		return nil, errorsmod.Wrap(err, "failed to sync curator bonded-role config to rep")
	}

	return &types.MsgUpdateOperationalParamsResponse{}, nil
}

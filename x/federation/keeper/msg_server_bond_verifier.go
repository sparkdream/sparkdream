package keeper

import (
	"context"

	"sparkdream/x/federation/types"

	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func (k msgServer) BondVerifier(ctx context.Context, msg *types.MsgBondVerifier) (*types.MsgBondVerifierResponse, error) {
	creatorBytes, err := k.addressCodec.StringToBytes(msg.Creator)
	if err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}
	_ = creatorBytes // used in trust level check below

	params, err := k.Params.Get(ctx)
	if err != nil {
		return nil, err
	}

	// 1. Verify creator meets min_verifier_trust_level via x/rep
	if k.late.repKeeper != nil {
		trustLevel, err := k.late.repKeeper.GetTrustLevel(ctx, sdk.AccAddress(creatorBytes))
		if err != nil {
			return nil, errorsmod.Wrap(types.ErrTrustLevelInsufficient, "failed to get trust level")
		}
		if uint32(trustLevel) < params.MinVerifierTrustLevel {
			return nil, errorsmod.Wrapf(types.ErrTrustLevelInsufficient, "trust level %d < required %d", trustLevel, params.MinVerifierTrustLevel)
		}
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	blockTime := sdkCtx.BlockTime().Unix()

	// 2. Check demotion cooldown if re-bonding
	existing, err := k.Verifiers.Get(ctx, msg.Creator)
	if err == nil {
		// Existing verifier — check demotion cooldown
		if existing.DemotionCooldownUntil > 0 && blockTime < existing.DemotionCooldownUntil {
			return nil, errorsmod.Wrapf(types.ErrDemotionCooldown, "cooldown until %d, current time %d", existing.DemotionCooldownUntil, blockTime)
		}
	} else {
		// New verifier
		existing = types.FederationVerifier{
			Address:  msg.Creator,
			BondedAt: blockTime,
			CurrentBond: math.ZeroInt(),
			TotalCommittedBond: math.ZeroInt(),
		}
	}

	// 3. Transfer DREAM from creator to federation module (TODO: actual DREAM transfer via x/rep)
	// For now, just track the bond amount

	// 4. Increment current_bond
	existing.CurrentBond = existing.CurrentBond.Add(msg.Amount)

	// 5. Update bond_status
	existing.BondStatus = k.calculateBondStatus(existing.CurrentBond, params)

	if err := k.Verifiers.Set(ctx, msg.Creator, existing); err != nil {
		return nil, err
	}

	// 6. Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(types.EventTypeVerifierBonded,
			sdk.NewAttribute(types.AttributeKeyLocalAddress, msg.Creator),
			sdk.NewAttribute(types.AttributeKeyAmount, msg.Amount.String()),
			sdk.NewAttribute(types.AttributeKeyBondStatus, existing.BondStatus.String())),
	)

	return &types.MsgBondVerifierResponse{}, nil
}

// calculateBondStatus determines the verifier bond status based on current bond amount.
func (k msgServer) calculateBondStatus(currentBond math.Int, params types.Params) types.VerifierBondStatus {
	if currentBond.GTE(params.MinVerifierBond) {
		return types.VerifierBondStatus_VERIFIER_BOND_STATUS_NORMAL
	}
	if currentBond.GTE(params.VerifierRecoveryThreshold) {
		return types.VerifierBondStatus_VERIFIER_BOND_STATUS_RECOVERY
	}
	return types.VerifierBondStatus_VERIFIER_BOND_STATUS_DEMOTED
}

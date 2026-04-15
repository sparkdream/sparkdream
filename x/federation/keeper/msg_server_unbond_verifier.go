package keeper

import (
	"context"

	"sparkdream/x/federation/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func (k msgServer) UnbondVerifier(ctx context.Context, msg *types.MsgUnbondVerifier) (*types.MsgUnbondVerifierResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	// 1. Get verifier
	verifier, err := k.Verifiers.Get(ctx, msg.Creator)
	if err != nil {
		return nil, errorsmod.Wrapf(types.ErrVerifierNotFound, "verifier %s not found", msg.Creator)
	}

	// 2. Verify amount <= current_bond - total_committed_bond
	availableBond := verifier.CurrentBond.Sub(verifier.TotalCommittedBond)
	if msg.Amount.GT(availableBond) {
		return nil, errorsmod.Wrapf(types.ErrBondCommitted, "requested %s but only %s available (committed: %s)", msg.Amount, availableBond, verifier.TotalCommittedBond)
	}

	// 3. Transfer DREAM back to creator (TODO: actual DREAM transfer via x/rep)

	// 4. Update current_bond
	verifier.CurrentBond = verifier.CurrentBond.Sub(msg.Amount)

	params, err := k.Params.Get(ctx)
	if err != nil {
		return nil, err
	}

	// 5. Recalculate bond_status
	newStatus := k.calculateBondStatus(verifier.CurrentBond, params)

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	blockTime := sdkCtx.BlockTime().Unix()

	// If transitioning to DEMOTED, set demotion cooldown
	if newStatus == types.VerifierBondStatus_VERIFIER_BOND_STATUS_DEMOTED &&
		verifier.BondStatus != types.VerifierBondStatus_VERIFIER_BOND_STATUS_DEMOTED {
		verifier.DemotionCooldownUntil = blockTime + int64(params.VerifierDemotionCooldown.Seconds())

		sdkCtx.EventManager().EmitEvent(
			sdk.NewEvent(types.EventTypeVerifierDemoted,
				sdk.NewAttribute(types.AttributeKeyLocalAddress, msg.Creator),
				sdk.NewAttribute(types.AttributeKeyRemainingBond, verifier.CurrentBond.String())),
		)
	}

	verifier.BondStatus = newStatus

	// 6. If current_bond == 0, delete (but preserve demotion cooldown via the record)
	if verifier.CurrentBond.IsZero() && verifier.TotalCommittedBond.IsZero() {
		// Keep the record if there's a demotion cooldown, otherwise delete
		if verifier.DemotionCooldownUntil > blockTime {
			if err := k.Verifiers.Set(ctx, msg.Creator, verifier); err != nil {
				return nil, err
			}
		} else {
			if err := k.Verifiers.Remove(ctx, msg.Creator); err != nil {
				return nil, err
			}
		}
	} else {
		if err := k.Verifiers.Set(ctx, msg.Creator, verifier); err != nil {
			return nil, err
		}
	}

	// 7. Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(types.EventTypeVerifierUnbonded,
			sdk.NewAttribute(types.AttributeKeyLocalAddress, msg.Creator),
			sdk.NewAttribute(types.AttributeKeyAmount, msg.Amount.String()),
			sdk.NewAttribute(types.AttributeKeyBondStatus, verifier.BondStatus.String())),
	)

	return &types.MsgUnbondVerifierResponse{}, nil
}

package keeper

import (
	"bytes"
	"context"
	"fmt"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/shield/types"
)

func (k msgServer) TriggerDKG(ctx context.Context, msg *types.MsgTriggerDkg) (*types.MsgTriggerDkgResponse, error) {
	authority, err := k.addressCodec.StringToBytes(msg.Authority)
	if err != nil {
		return nil, errorsmod.Wrap(err, "invalid authority address")
	}
	if !bytes.Equal(k.GetAuthority(), authority) {
		expectedAuthorityStr, _ := k.addressCodec.BytesToString(k.GetAuthority())
		return nil, errorsmod.Wrapf(types.ErrInvalidSigner, "invalid authority; expected %s, got %s", expectedAuthorityStr, msg.Authority)
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Refuse while a ceremony is running (any phase between REGISTERING and
	// ACTIVE); a re-key during COMPLAINING/JUSTIFYING would silently discard
	// the round's complaints and contributions.
	if existing, found := k.GetDKGStateVal(ctx); found {
		switch existing.Phase {
		case types.DKGPhase_DKG_PHASE_REGISTERING, types.DKGPhase_DKG_PHASE_CONTRIBUTING,
			types.DKGPhase_DKG_PHASE_COMPLAINING, types.DKGPhase_DKG_PHASE_JUSTIFYING:
			return nil, errorsmod.Wrap(types.ErrDKGInProgress, fmt.Sprintf("round %d is in phase %s", existing.Round, existing.Phase.String()))
		}
	}

	// Snapshot bonded validators
	if k.late.stakingKeeper == nil {
		return nil, errorsmod.Wrap(types.ErrInsufficientValidators, "staking keeper not wired")
	}

	bondedVals, err := k.late.stakingKeeper.GetBondedValidatorsByPower(ctx)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to get bonded validators")
	}

	params, err := k.Params.Get(ctx)
	if err != nil {
		return nil, err
	}

	if len(bondedVals) < int(params.MinTleValidators) {
		return nil, errorsmod.Wrapf(types.ErrInsufficientValidators,
			"need at least %d bonded validators, have %d", params.MinTleValidators, len(bondedVals))
	}

	// Build expected validator list
	var expectedValidators []string
	for _, v := range bondedVals {
		addr := v.GetOperator()
		if addr == "" {
			continue
		}
		expectedValidators = append(expectedValidators, addr)
	}

	// Determine threshold
	numerator := msg.ThresholdNumerator
	denominator := msg.ThresholdDenominator
	if numerator == 0 {
		numerator = 2
	}
	if denominator == 0 {
		denominator = 3
	}
	// A threshold above the participant count can never be met, so the round
	// could only fail; the denominator bound keeps ComputeThreshold's
	// numerator*total product far from overflow.
	if numerator > denominator || denominator > types.MaxDKGThresholdDenominator {
		return nil, errorsmod.Wrapf(types.ErrInvalidDKGThreshold, "got %d/%d", numerator, denominator)
	}

	// Determine the round number
	var round uint64 = 1
	if existing, found := k.GetDKGStateVal(ctx); found {
		round = existing.Round + 1
	}

	height := sdkCtx.BlockHeight()

	// Create DKG state in REGISTERING phase; the window is split into the
	// four ceremony phases (see newDKGState).
	dkgState := newDKGState(round, height, params.DkgWindowBlocks, numerator, denominator, expectedValidators)
	if err := k.SetDKGStateVal(ctx, dkgState); err != nil {
		return nil, err
	}

	// Clear every per-round record of the previous round: a stale
	// registration would have dealers encrypt evaluations to a key the
	// validator no longer uses.
	if err := k.clearDKGRound(ctx); err != nil {
		return nil, err
	}

	// Retire everything derived from the previous key set: its decryption
	// shares and epoch keys are useless under the new one, and pending ops
	// encrypted to it can never be opened, so they are dropped now with an
	// expiry event rather than lingering until max_pending_epochs.
	dropped, err := k.RetireKeySetMaterial(ctx)
	if err != nil {
		return nil, err
	}
	if dropped > 0 {
		sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
			types.EventTypeShieldBatchExpired,
			sdk.NewAttribute(types.AttributeKeyCount, fmt.Sprintf("%d", dropped)),
			sdk.NewAttribute(types.AttributeKeyDKGRound, fmt.Sprintf("%d", round)),
		))
	}

	// Disable encrypted batch mode until DKG completes
	params.EncryptedBatchEnabled = false
	if err := k.Params.Set(ctx, params); err != nil {
		return nil, err
	}

	sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeShieldDKGOpened,
		sdk.NewAttribute(types.AttributeKeyDKGRound, fmt.Sprintf("%d", round)),
		sdk.NewAttribute(types.AttributeKeyDKGPhase, types.DKGPhase_DKG_PHASE_REGISTERING.String()),
		sdk.NewAttribute(types.AttributeKeyCount, fmt.Sprintf("%d", len(expectedValidators))),
	))

	return &types.MsgTriggerDkgResponse{}, nil
}

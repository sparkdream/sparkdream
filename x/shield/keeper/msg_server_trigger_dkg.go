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

	// Check no DKG is already in progress (REGISTERING or CONTRIBUTING)
	if existing, found := k.GetDKGStateVal(ctx); found {
		if existing.Phase == types.DKGPhase_DKG_PHASE_REGISTERING || existing.Phase == types.DKGPhase_DKG_PHASE_CONTRIBUTING {
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

	if uint32(len(bondedVals)) < params.MinTleValidators {
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

	// Determine the round number
	var round uint64 = 1
	if existing, found := k.GetDKGStateVal(ctx); found {
		round = existing.Round + 1
	}

	height := sdkCtx.BlockHeight()

	halfWindow := int64(params.DkgWindowBlocks / 2)

	// Create DKG state in REGISTERING phase.
	// First half of the DKG window is for key registration, second half for contributions.
	dkgState := types.DKGState{
		Round:                 round,
		Phase:                 types.DKGPhase_DKG_PHASE_REGISTERING,
		OpenAtHeight:          height,
		RegistrationDeadline:  height + halfWindow,
		ContributionDeadline:  height + halfWindow*2,
		ThresholdNumerator:    numerator,
		ThresholdDenominator:  denominator,
		ExpectedValidators:    expectedValidators,
		ContributionsReceived: 0,
	}

	if err := k.SetDKGStateVal(ctx, dkgState); err != nil {
		return nil, err
	}

	// Clear any old contributions from a previous round
	if err := k.ClearDKGContributions(ctx); err != nil {
		return nil, err
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

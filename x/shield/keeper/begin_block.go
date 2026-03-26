package keeper

import (
	"context"
	"fmt"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/shield/types"
)

// BeginBlocker handles auto-funding and DKG state machine transitions.
func (k Keeper) BeginBlocker(ctx context.Context) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	params, err := k.Params.Get(ctx)
	if err != nil {
		return nil
	}

	if !params.Enabled {
		return nil
	}

	// --- Auto-fund shield module from community pool ---
	k.autoFundModule(ctx, sdkCtx, params)

	// --- DKG state machine ---
	k.advanceDKGStateMachine(ctx, sdkCtx, params)

	return nil
}

// autoFundModule handles the shield module gas reserve funding from the community pool.
func (k Keeper) autoFundModule(ctx context.Context, sdkCtx sdk.Context, params types.Params) {
	moduleAddr := k.accountKeeper.GetModuleAddress(types.ModuleName)
	balance := k.bankKeeper.GetBalance(ctx, moduleAddr, "uspark")

	if balance.Amount.GTE(params.MinGasReserve) {
		return
	}

	gap := params.MinGasReserve.Sub(balance.Amount)

	day := uint64(sdkCtx.BlockHeight()) / 14400
	funded := k.GetDayFunding(ctx, day)
	remaining := params.MaxFundingPerDay.Sub(funded)

	if remaining.IsZero() || !remaining.IsPositive() {
		sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
			types.EventShieldFundingCapReached,
			sdk.NewAttribute(types.AttributeKeyDay, fmt.Sprintf("%d", day)),
			sdk.NewAttribute(types.AttributeKeyTotalFunded, funded.String()),
			sdk.NewAttribute(types.AttributeKeyCap, params.MaxFundingPerDay.String()),
		))
		return
	}

	fundAmount := math.MinInt(gap, remaining)

	if k.late.distrKeeper == nil {
		return
	}

	// Check community pool balance before attempting distribution
	pool, err := k.late.distrKeeper.GetCommunityPool(ctx)
	if err != nil {
		return
	}
	available := pool.AmountOf("uspark").TruncateInt()
	if !available.IsPositive() || available.LT(fundAmount) {
		return
	}

	coins := sdk.NewCoins(sdk.NewCoin("uspark", fundAmount))
	err = k.late.distrKeeper.DistributeFromFeePool(ctx, coins, moduleAddr)
	if err != nil {
		sdkCtx.Logger().With("module", "x/shield").Info(
			"Failed to fund shield module from community pool",
			"requested", fundAmount.String(),
			"err", err,
		)
		return
	}

	_ = k.SetDayFunding(ctx, day, funded.Add(fundAmount))

	sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeShieldFunded,
		sdk.NewAttribute(types.AttributeKeyAmount, fundAmount.String()),
		sdk.NewAttribute(types.AttributeKeyDay, fmt.Sprintf("%d", day)),
		sdk.NewAttribute(types.AttributeKeyNewBalance, balance.Amount.Add(fundAmount).String()),
	))
}

// advanceDKGStateMachine manages the DKG lifecycle:
//   - INACTIVE: auto-trigger when bonded validators >= min_tle_validators
//   - REGISTERING: transition to CONTRIBUTING when registration deadline expires
//   - CONTRIBUTING: finalize when all contributions received or contribution deadline expires
//   - ACTIVE: detect validator set drift and trigger re-keying if needed
func (k Keeper) advanceDKGStateMachine(ctx context.Context, sdkCtx sdk.Context, params types.Params) {
	dkgState, found := k.GetDKGStateVal(ctx)
	height := sdkCtx.BlockHeight()

	switch {
	case !found || dkgState.Phase == types.DKGPhase_DKG_PHASE_INACTIVE:
		k.dkgCheckAutoTrigger(ctx, sdkCtx, params)

	case dkgState.Phase == types.DKGPhase_DKG_PHASE_REGISTERING:
		k.dkgAdvanceRegistering(ctx, sdkCtx, params, dkgState, height)

	case dkgState.Phase == types.DKGPhase_DKG_PHASE_CONTRIBUTING:
		k.dkgAdvanceContributing(ctx, sdkCtx, dkgState, height)

	case dkgState.Phase == types.DKGPhase_DKG_PHASE_ACTIVE:
		k.dkgCheckDrift(ctx, sdkCtx, params, dkgState)
	}
}

// dkgCheckAutoTrigger starts a new DKG ceremony if enough validators exist.
func (k Keeper) dkgCheckAutoTrigger(ctx context.Context, sdkCtx sdk.Context, params types.Params) {
	if k.late.stakingKeeper == nil || params.MinTleValidators == 0 {
		return
	}

	bondedVals, err := k.late.stakingKeeper.GetBondedValidatorsByPower(ctx)
	if err != nil || uint32(len(bondedVals)) < params.MinTleValidators {
		return
	}

	// Don't auto-trigger if we already have an active key set
	if _, hasKeySet := k.GetTLEKeySetVal(ctx); hasKeySet {
		return
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

	if uint32(len(expectedValidators)) < params.MinTleValidators {
		return
	}

	// Determine round number
	var round uint64 = 1
	if existing, found := k.GetDKGStateVal(ctx); found {
		round = existing.Round + 1
	}

	height := sdkCtx.BlockHeight()
	halfWindow := int64(params.DkgWindowBlocks / 2)

	dkgState := types.DKGState{
		Round:                 round,
		Phase:                 types.DKGPhase_DKG_PHASE_REGISTERING,
		OpenAtHeight:          height,
		RegistrationDeadline:  height + halfWindow,
		ContributionDeadline:  height + halfWindow*2,
		ThresholdNumerator:    2,
		ThresholdDenominator:  3,
		ExpectedValidators:    expectedValidators,
		ContributionsReceived: 0,
	}

	if err := k.SetDKGStateVal(ctx, dkgState); err != nil {
		return
	}
	_ = k.ClearDKGContributions(ctx)
	_ = k.ClearDKGRegistrations(ctx)

	sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeShieldDKGOpened,
		sdk.NewAttribute(types.AttributeKeyDKGRound, fmt.Sprintf("%d", round)),
		sdk.NewAttribute(types.AttributeKeyDKGPhase, types.DKGPhase_DKG_PHASE_REGISTERING.String()),
		sdk.NewAttribute(types.AttributeKeyCount, fmt.Sprintf("%d", len(expectedValidators))),
	))
}

// dkgAdvanceRegistering transitions REGISTERING → CONTRIBUTING when the registration deadline expires.
func (k Keeper) dkgAdvanceRegistering(ctx context.Context, sdkCtx sdk.Context, params types.Params, dkgState types.DKGState, height int64) {
	if height < dkgState.RegistrationDeadline {
		return // Window still open
	}

	// Transition to CONTRIBUTING phase
	dkgState.Phase = types.DKGPhase_DKG_PHASE_CONTRIBUTING
	// ContributionDeadline was already set at DKG open time

	if err := k.SetDKGStateVal(ctx, dkgState); err != nil {
		return
	}

	sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeShieldDKGOpened,
		sdk.NewAttribute(types.AttributeKeyDKGRound, fmt.Sprintf("%d", dkgState.Round)),
		sdk.NewAttribute(types.AttributeKeyDKGPhase, types.DKGPhase_DKG_PHASE_CONTRIBUTING.String()),
	))
}

// dkgAdvanceContributing finalizes DKG when contributions are complete or contribution deadline expires.
func (k Keeper) dkgAdvanceContributing(ctx context.Context, sdkCtx sdk.Context, dkgState types.DKGState, height int64) {
	allReceived := dkgState.ContributionsReceived >= uint64(len(dkgState.ExpectedValidators))
	windowExpired := height >= dkgState.ContributionDeadline

	if !allReceived && !windowExpired {
		return // Still waiting for contributions
	}

	// Check if we have enough contributions to meet threshold
	threshold := computeDKGThreshold(dkgState)
	if dkgState.ContributionsReceived < threshold {
		k.dkgFail(ctx, sdkCtx, dkgState, fmt.Sprintf(
			"insufficient contributions: have %d, need %d", dkgState.ContributionsReceived, threshold))
		return
	}

	// Aggregate contributions to compute the TLE key set
	contributions := k.GetAllDKGContributions(ctx)
	keySet, err := AggregateFeldmanDKGFromContributions(contributions, dkgState)
	if err != nil {
		k.dkgFail(ctx, sdkCtx, dkgState, fmt.Sprintf("aggregation failed: %s", err.Error()))
		return
	}

	keySet.CreatedAtHeight = height

	// Store the new TLE key set
	if err := k.SetTLEKeySetVal(ctx, *keySet); err != nil {
		return
	}

	// Transition to ACTIVE
	dkgState.Phase = types.DKGPhase_DKG_PHASE_ACTIVE
	if err := k.SetDKGStateVal(ctx, dkgState); err != nil {
		return
	}

	// Enable encrypted batch mode now that we have a key set
	currentParams, err := k.Params.Get(ctx)
	if err == nil {
		currentParams.EncryptedBatchEnabled = true
		_ = k.Params.Set(ctx, currentParams)
	}

	sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeShieldDKGComplete,
		sdk.NewAttribute(types.AttributeKeyDKGRound, fmt.Sprintf("%d", dkgState.Round)),
		sdk.NewAttribute(types.AttributeKeyContributionsCount, fmt.Sprintf("%d", dkgState.ContributionsReceived)),
	))

	sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeShieldDKGActivated,
		sdk.NewAttribute(types.AttributeKeyDKGRound, fmt.Sprintf("%d", dkgState.Round)),
	))
}

// dkgCheckDrift detects validator set drift and triggers re-keying if needed.
func (k Keeper) dkgCheckDrift(ctx context.Context, sdkCtx sdk.Context, params types.Params, dkgState types.DKGState) {
	if params.MaxValidatorSetDrift == 0 {
		return // Drift detection disabled
	}

	drift := k.detectValidatorSetDrift(ctx, dkgState)
	if drift < params.MaxValidatorSetDrift {
		return // Within tolerance
	}

	sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeShieldValidatorDrift,
		sdk.NewAttribute(types.AttributeKeyDriftPercent, fmt.Sprintf("%d", drift)),
		sdk.NewAttribute(types.AttributeKeyDKGRound, fmt.Sprintf("%d", dkgState.Round)),
	))

	// Trigger re-keying: reset to INACTIVE so auto-trigger fires next block
	dkgState.Phase = types.DKGPhase_DKG_PHASE_INACTIVE
	_ = k.SetDKGStateVal(ctx, dkgState)

	// Clear the old key set so auto-trigger will fire
	_ = k.TLEKeySet.Remove(ctx)

	// Disable encrypted batch until new DKG completes
	currentParams, err := k.Params.Get(ctx)
	if err == nil {
		currentParams.EncryptedBatchEnabled = false
		_ = k.Params.Set(ctx, currentParams)
	}
}

// dkgFail handles a failed DKG round by resetting to INACTIVE.
func (k Keeper) dkgFail(ctx context.Context, sdkCtx sdk.Context, dkgState types.DKGState, reason string) {
	sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeShieldDKGFailed,
		sdk.NewAttribute(types.AttributeKeyDKGRound, fmt.Sprintf("%d", dkgState.Round)),
		sdk.NewAttribute(types.AttributeKeyError, reason),
	))

	sdkCtx.Logger().With("module", "x/shield").Error(
		"DKG round failed",
		"round", dkgState.Round,
		"reason", reason,
	)

	// Reset to INACTIVE — auto-trigger will retry next block if conditions met
	dkgState.Phase = types.DKGPhase_DKG_PHASE_INACTIVE
	dkgState.ContributionsReceived = 0
	_ = k.SetDKGStateVal(ctx, dkgState)
	_ = k.ClearDKGContributions(ctx)
	_ = k.ClearDKGRegistrations(ctx)
}

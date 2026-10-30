package keeper

import (
	"context"
	"fmt"
	commontypes "sparkdream/x/common/types"

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

// fundingDay is the daily-cap bucket: the UTC day of the block time.
func fundingDay(sdkCtx sdk.Context) uint64 {
	return commontypes.Saturate[uint64](sdkCtx.BlockTime().Unix()) / 86400
}

// autoFundModule handles the shield module gas reserve funding from the community pool.
func (k Keeper) autoFundModule(ctx context.Context, sdkCtx sdk.Context, params types.Params) {
	moduleAddr := k.accountKeeper.GetModuleAddress(types.ModuleName)
	balance := k.bankKeeper.GetBalance(ctx, moduleAddr, k.BondDenom(ctx))
	if balance.Amount.IsNil() {
		balance.Amount = math.ZeroInt()
	}

	if balance.Amount.GTE(params.MinGasReserve) {
		return
	}

	gap := params.MinGasReserve.Sub(balance.Amount)

	day := fundingDay(sdkCtx)
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
	available := pool.AmountOf(k.BondDenom(ctx)).TruncateInt()
	if !available.IsPositive() || available.LT(fundAmount) {
		return
	}

	coins := sdk.NewCoins(sdk.NewCoin(k.BondDenom(ctx), fundAmount))
	err = k.late.distrKeeper.DistributeFromFeePool(ctx, coins, moduleAddr)
	if err != nil {
		sdkCtx.Logger().With("module", "x/shield").Info(
			"Failed to fund shield module from community pool",
			"requested", fundAmount.String(),
			"err", err,
		)
		return
	}

	if err := k.SetDayFunding(ctx, day, funded.Add(fundAmount)); err != nil {
		sdkCtx.Logger().With("module", "x/shield").Error(
			"Failed to update day funding ledger",
			"day", day,
			"err", err,
		)
	}

	sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeShieldFunded,
		sdk.NewAttribute(types.AttributeKeyAmount, fundAmount.String()),
		sdk.NewAttribute(types.AttributeKeyDay, fmt.Sprintf("%d", day)),
		sdk.NewAttribute(types.AttributeKeyNewBalance, balance.Amount.Add(fundAmount).String()),
	))
}

// advanceDKGStateMachine manages the DKG lifecycle:
//   - INACTIVE: auto-trigger when bonded validators >= min_tle_validators
//   - REGISTERING: at the deadline, narrow the participant set to the
//     validators that registered a key and move to CONTRIBUTING
//   - CONTRIBUTING: at the deadline (or once every participant dealt), move
//     to COMPLAINING if the threshold was met
//   - COMPLAINING: at the deadline, move to JUSTIFYING
//   - JUSTIFYING: at the deadline, disqualify dealers with unanswered
//     complaints, aggregate the key set, move to ACTIVE
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

	case dkgState.Phase == types.DKGPhase_DKG_PHASE_COMPLAINING:
		k.dkgAdvanceComplaining(ctx, sdkCtx, dkgState, height)

	case dkgState.Phase == types.DKGPhase_DKG_PHASE_JUSTIFYING:
		k.dkgAdvanceJustifying(ctx, sdkCtx, params, dkgState, height)

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
	if err != nil || len(bondedVals) < int(params.MinTleValidators) {
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

	if len(expectedValidators) < int(params.MinTleValidators) {
		return
	}

	// Determine round number. Only bump the round when the previous DKG
	// actually completed (received at least one contribution); otherwise
	// re-use the existing round so repeated INACTIVE→REGISTERING entries
	// with no progress don't inflate the counter.
	var round uint64 = 1
	if existing, found := k.GetDKGStateVal(ctx); found {
		if existing.Phase == types.DKGPhase_DKG_PHASE_INACTIVE && existing.ContributionsReceived > 0 {
			round = existing.Round + 1
		} else {
			round = existing.Round
			if round == 0 {
				round = 1
			}
		}
	}

	dkgState := newDKGState(round, sdkCtx.BlockHeight(), params.DkgWindowBlocks, 2, 3, expectedValidators)
	if err := k.SetDKGStateVal(ctx, dkgState); err != nil {
		return
	}
	if err := k.clearDKGRound(ctx); err != nil {
		sdk.UnwrapSDKContext(sdkCtx).Logger().Error("dkgCheckAutoTrigger: k.clearDKGRound failed", "err", err)
	}

	sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeShieldDKGOpened,
		sdk.NewAttribute(types.AttributeKeyDKGRound, fmt.Sprintf("%d", round)),
		sdk.NewAttribute(types.AttributeKeyDKGPhase, types.DKGPhase_DKG_PHASE_REGISTERING.String()),
		sdk.NewAttribute(types.AttributeKeyCount, fmt.Sprintf("%d", len(expectedValidators))),
	))
}

// dkgAdvanceRegistering transitions REGISTERING → CONTRIBUTING when the
// registration deadline expires. The participant set is narrowed to the
// validators that registered a key for this round: nobody can encrypt an
// evaluation to a validator without one, so keeping it would give it a public
// share it can never serve (and a liveness miss every demanded epoch).
func (k Keeper) dkgAdvanceRegistering(ctx context.Context, sdkCtx sdk.Context, params types.Params, dkgState types.DKGState, height int64) {
	if height < dkgState.RegistrationDeadline {
		return // Window still open
	}

	var registered []string
	for _, v := range dkgState.ExpectedValidators {
		if reg, found := k.GetDKGRegistration(ctx, v); found && reg.Round == dkgState.Round {
			registered = append(registered, v)
		}
	}
	if len(registered) == 0 || len(registered) < int(params.MinTleValidators) {
		k.dkgFail(ctx, sdkCtx, dkgState, fmt.Sprintf(
			"insufficient registrations: have %d, need %d", len(registered), params.MinTleValidators))
		return
	}
	dkgState.ExpectedValidators = registered
	dkgState.Phase = types.DKGPhase_DKG_PHASE_CONTRIBUTING

	if err := k.SetDKGStateVal(ctx, dkgState); err != nil {
		return
	}

	sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeShieldDKGOpened,
		sdk.NewAttribute(types.AttributeKeyDKGRound, fmt.Sprintf("%d", dkgState.Round)),
		sdk.NewAttribute(types.AttributeKeyDKGPhase, types.DKGPhase_DKG_PHASE_CONTRIBUTING.String()),
		sdk.NewAttribute(types.AttributeKeyCount, fmt.Sprintf("%d", len(registered))),
	))
}

// dkgAdvanceContributing moves CONTRIBUTING → COMPLAINING once every
// participant has dealt or the contribution deadline expires, provided the
// threshold of contributions was met.
func (k Keeper) dkgAdvanceContributing(ctx context.Context, sdkCtx sdk.Context, dkgState types.DKGState, height int64) {
	allReceived := dkgState.ContributionsReceived >= uint64(len(dkgState.ExpectedValidators))
	windowExpired := height >= dkgState.ContributionDeadline

	if !allReceived && !windowExpired {
		return // Still waiting for contributions
	}

	threshold := computeDKGThreshold(dkgState)
	if dkgState.ContributionsReceived < threshold {
		k.dkgFail(ctx, sdkCtx, dkgState, fmt.Sprintf(
			"insufficient contributions: have %d, need %d", dkgState.ContributionsReceived, threshold))
		return
	}

	// The complaint window stays anchored to the original schedule
	// (complaint_deadline), so an early finish here only lengthens it.
	dkgState.Phase = types.DKGPhase_DKG_PHASE_COMPLAINING
	if err := k.SetDKGStateVal(ctx, dkgState); err != nil {
		return
	}
	sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeShieldDKGOpened,
		sdk.NewAttribute(types.AttributeKeyDKGRound, fmt.Sprintf("%d", dkgState.Round)),
		sdk.NewAttribute(types.AttributeKeyDKGPhase, types.DKGPhase_DKG_PHASE_COMPLAINING.String()),
	))
}

// dkgAdvanceComplaining moves COMPLAINING → JUSTIFYING at the complaint
// deadline. Absence of complaints cannot be observed early (an empty report
// carries nothing), so the window always runs its course.
func (k Keeper) dkgAdvanceComplaining(ctx context.Context, sdkCtx sdk.Context, dkgState types.DKGState, height int64) {
	if height < dkgState.ComplaintDeadline {
		return
	}
	dkgState.Phase = types.DKGPhase_DKG_PHASE_JUSTIFYING
	if err := k.SetDKGStateVal(ctx, dkgState); err != nil {
		return
	}
	sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeShieldDKGOpened,
		sdk.NewAttribute(types.AttributeKeyDKGRound, fmt.Sprintf("%d", dkgState.Round)),
		sdk.NewAttribute(types.AttributeKeyDKGPhase, types.DKGPhase_DKG_PHASE_JUSTIFYING.String()),
		sdk.NewAttribute(types.AttributeKeyCount, fmt.Sprintf("%d", len(k.GetAllDKGComplaints(ctx)))),
	))
}

// dkgAdvanceJustifying finalizes the ceremony at the justification deadline:
// a dealer with a complaint it did not answer with a valid evaluation is
// disqualified and its contribution dropped; the remaining (qualified)
// contributions must still meet the threshold; the key set is aggregated
// from them and the round goes ACTIVE.
func (k Keeper) dkgAdvanceJustifying(ctx context.Context, sdkCtx sdk.Context, params types.Params, dkgState types.DKGState, height int64) {
	if height < dkgState.JustificationDeadline {
		return
	}

	disqualified := map[string]bool{}
	for _, c := range k.GetAllDKGComplaints(ctx) {
		if !c.Justified {
			disqualified[c.DealerAddress] = true
		}
	}
	dkgState.DisqualifiedDealers = nil
	for _, v := range dkgState.ExpectedValidators {
		if disqualified[v] {
			dkgState.DisqualifiedDealers = append(dkgState.DisqualifiedDealers, v)
			sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
				types.EventTypeShieldDKGDisqualified,
				sdk.NewAttribute(types.AttributeKeyDealer, v),
				sdk.NewAttribute(types.AttributeKeyDKGRound, fmt.Sprintf("%d", dkgState.Round)),
			))
		}
	}

	contributions := k.QualifiedDKGContributions(ctx, dkgState)
	threshold := computeDKGThreshold(dkgState)
	if uint64(len(contributions)) < threshold {
		k.dkgFail(ctx, sdkCtx, dkgState, fmt.Sprintf(
			"insufficient qualified contributions: have %d, need %d", len(contributions), threshold))
		return
	}

	keySet, err := AggregateFeldmanDKGFromContributions(contributions, dkgState)
	if err != nil {
		k.dkgFail(ctx, sdkCtx, dkgState, fmt.Sprintf("aggregation failed: %s", err.Error()))
		return
	}
	keySet.CreatedAtHeight = height

	if err := k.SetTLEKeySetVal(ctx, *keySet); err != nil {
		return
	}
	// A new key set starts with a clean liveness record.
	if err := k.ResetTLELiveness(ctx); err != nil {
		sdk.UnwrapSDKContext(sdkCtx).Logger().Error("dkgAdvanceJustifying: k.ResetTLELiveness failed", "err", err)
	}

	dkgState.Phase = types.DKGPhase_DKG_PHASE_ACTIVE
	if err := k.SetDKGStateVal(ctx, dkgState); err != nil {
		return
	}

	// Batch mode is a governance decision: it turns on by itself only when
	// enable_batch_after_dkg says so.
	if params.EnableBatchAfterDkg {
		if currentParams, err := k.Params.Get(ctx); err == nil {
			currentParams.EncryptedBatchEnabled = true
			if err := k.Params.Set(ctx, currentParams); err != nil {
				sdkCtx.Logger().Error("dkgAdvanceJustifying: Params.Set failed", "err", err)
			}
		}
	}

	sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeShieldDKGComplete,
		sdk.NewAttribute(types.AttributeKeyDKGRound, fmt.Sprintf("%d", dkgState.Round)),
		sdk.NewAttribute(types.AttributeKeyContributionsCount, fmt.Sprintf("%d", len(contributions))),
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
	if err := k.SetDKGStateVal(ctx, dkgState); err != nil {
		sdkCtx.Logger().Error("dkgCheckDrift: k.SetDKGStateVal failed", "err", err)
	}

	// Retire the old key set and everything sealed to it (like MsgTriggerDKG
	// does), so nothing lingers encrypted to a key no one will serve.
	if err := k.TLEKeySet.Remove(ctx); err != nil {
		sdkCtx.Logger().Error("dkgCheckDrift: TLEKeySet.Remove failed", "err", err)
	}
	if dropped, err := k.RetireKeySetMaterial(ctx); err == nil && dropped > 0 {
		sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
			types.EventTypeShieldBatchExpired,
			sdk.NewAttribute(types.AttributeKeyCount, fmt.Sprintf("%d", dropped)),
			sdk.NewAttribute(types.AttributeKeyDKGRound, fmt.Sprintf("%d", dkgState.Round)),
		))
	}

	// Disable encrypted batch until new DKG completes
	currentParams, err := k.Params.Get(ctx)
	if err == nil {
		currentParams.EncryptedBatchEnabled = false
		if err := k.Params.Set(ctx, currentParams); err != nil {
			sdkCtx.Logger().Error("dkgCheckDrift: Params.Set failed", "err", err)
		}
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
	dkgState.DisqualifiedDealers = nil
	if err := k.SetDKGStateVal(ctx, dkgState); err != nil {
		sdkCtx.Logger().Error("dkgFail: k.SetDKGStateVal failed", "err", err)
	}
	if err := k.clearDKGRound(ctx); err != nil {
		sdk.UnwrapSDKContext(sdkCtx).Logger().Error("dkgFail: k.clearDKGRound failed", "err", err)
	}
}

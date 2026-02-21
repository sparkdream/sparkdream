package keeper

import (
	"context"
	"errors"
	"fmt"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/vote/types"
)

// trackTleLiveness checks whether TLE validators submitted decryption shares
// for the previous epoch. Called every block from ProcessEndBlock.
// Most blocks this is a single Has() check (O(1)) and returns immediately.
func (k Keeper) trackTleLiveness(ctx context.Context) error {
	params, err := k.Params.Get(ctx)
	if err != nil {
		return err
	}
	if !params.TleEnabled {
		return nil
	}

	currentEpoch := k.seasonKeeper.GetCurrentEpoch(ctx)
	if currentEpoch <= 0 {
		return nil // no completed epoch yet
	}
	prevEpoch := uint64(currentEpoch - 1)

	// Idempotency: skip if already recorded for this epoch.
	has, err := k.TleEpochParticipation.Has(ctx, prevEpoch)
	if err != nil {
		return err
	}
	if has {
		return nil
	}

	return k.recordEpochParticipation(ctx, prevEpoch, params)
}

// recordEpochParticipation checks all registered TLE validators for the given
// epoch, records who missed, emits events, stores the record, and prunes old data.
func (k Keeper) recordEpochParticipation(ctx context.Context, epoch uint64, params types.Params) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Collect all registered validators.
	var allValidators []string
	err := k.TleValidatorShare.Walk(ctx, nil, func(validator string, _ types.TleValidatorShare) (bool, error) {
		allValidators = append(allValidators, validator)
		return false, nil
	})
	if err != nil {
		return err
	}

	if len(allValidators) == 0 {
		return nil // no registered validators, nothing to track
	}

	// Check each validator for decryption share submission.
	var missedValidators []string
	submittedCount := uint32(0)
	for _, validator := range allValidators {
		shareKey := tleShareKey(validator, epoch)
		has, err := k.TleDecryptionShare.Has(ctx, shareKey)
		if err != nil {
			return err
		}
		if has {
			submittedCount++
		} else {
			missedValidators = append(missedValidators, validator)
		}
	}

	registeredCount := uint32(len(allValidators))

	// Emit per-validator miss events.
	for _, validator := range missedValidators {
		sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
			types.EventTLEValidatorMissed,
			sdk.NewAttribute(types.AttributeValidator, validator),
			sdk.NewAttribute(types.AttributeEpoch, fmt.Sprintf("%d", epoch)),
		))
	}

	// Emit epoch summary event.
	sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTLEEpochParticipation,
		sdk.NewAttribute(types.AttributeEpoch, fmt.Sprintf("%d", epoch)),
		sdk.NewAttribute(types.AttributeRegisteredCount, fmt.Sprintf("%d", registeredCount)),
		sdk.NewAttribute(types.AttributeSubmittedCount, fmt.Sprintf("%d", submittedCount)),
		sdk.NewAttribute(types.AttributeMissedCount, fmt.Sprintf("%d", len(missedValidators))),
	))

	// Store the participation record.
	record := types.TleEpochParticipation{
		Epoch:            epoch,
		RegisteredCount:  registeredCount,
		SubmittedCount:   submittedCount,
		MissedValidators: missedValidators,
		CheckedAt:        sdkCtx.BlockHeight(),
	}
	if err := k.TleEpochParticipation.Set(ctx, epoch, record); err != nil {
		return err
	}

	// Update persisted per-validator liveness flags (Phase 2).
	if err := k.updateValidatorLivenessFlags(ctx, allValidators, params); err != nil {
		return err
	}

	// Prune records older than the miss window.
	return k.pruneTleParticipation(ctx, epoch, params)
}

// updateValidatorLivenessFlags recomputes per-validator miss counts across the
// current window, detects active↔inactive transitions, emits events, and persists
// the updated TleValidatorLiveness records.
func (k Keeper) updateValidatorLivenessFlags(ctx context.Context, allValidators []string, params types.Params) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	blockHeight := sdkCtx.BlockHeight()

	// Build per-validator miss counts from stored epoch participation records.
	missCount := make(map[string]uint32)
	var windowSize uint32
	err := k.TleEpochParticipation.Walk(ctx, nil, func(_ uint64, record types.TleEpochParticipation) (bool, error) {
		windowSize++
		for _, v := range record.MissedValidators {
			missCount[v]++
		}
		return false, nil
	})
	if err != nil {
		return err
	}

	for _, validator := range allValidators {
		missed := missCount[validator]
		nowActive := missed <= params.TleMissTolerance

		// Load existing record (if any) to detect transitions.
		existing, err := k.TleValidatorLiveness.Get(ctx, validator)
		wasActive := true // default: treat new validators as previously active
		if err != nil {
			if !errors.Is(err, collections.ErrNotFound) {
				return err
			}
			// No existing record — this is the first time we're tracking this validator.
		} else {
			wasActive = existing.TleActive
		}

		record := types.TleValidatorLiveness{
			Validator:   validator,
			TleActive:   nowActive,
			MissedCount: missed,
			WindowSize:  windowSize,
			FlaggedAt:   existing.FlaggedAt,
			RecoveredAt: existing.RecoveredAt,
		}

		// Detect transitions and emit events.
		if wasActive && !nowActive {
			// Active → Inactive transition.
			record.FlaggedAt = blockHeight
			sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
				types.EventTLEValidatorFlaggedInactive,
				sdk.NewAttribute(types.AttributeValidator, validator),
				sdk.NewAttribute(types.AttributeMissedCount, fmt.Sprintf("%d", missed)),
			))

			// Jail the validator if jailing is enabled (Phase 3).
			if params.TleJailEnabled {
				if err := k.jailTleValidator(ctx, validator, missed); err != nil {
					sdkCtx.Logger().Error("failed to jail TLE validator", "validator", validator, "error", err)
				}
			}
		} else if !wasActive && nowActive {
			// Inactive → Active transition (recovery).
			record.RecoveredAt = blockHeight
			sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
				types.EventTLEValidatorRecovered,
				sdk.NewAttribute(types.AttributeValidator, validator),
				sdk.NewAttribute(types.AttributeMissedCount, fmt.Sprintf("%d", missed)),
			))
		}

		if err := k.TleValidatorLiveness.Set(ctx, validator, record); err != nil {
			return err
		}
	}

	return nil
}

// jailTleValidator jails a validator for TLE inactivity. Converts the string
// validator address to a consensus address and calls the staking keeper's Jail.
// Idempotent: skips if the validator is already jailed.
func (k Keeper) jailTleValidator(ctx context.Context, validatorAddr string, missedCount uint32) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	addrBytes, err := k.addressCodec.StringToBytes(validatorAddr)
	if err != nil {
		return fmt.Errorf("invalid validator address: %w", err)
	}

	val, err := k.stakingKeeper.GetValidator(ctx, sdk.ValAddress(addrBytes))
	if err != nil {
		return fmt.Errorf("validator not found: %w", err)
	}

	if val.IsJailed() {
		return nil // already jailed, nothing to do
	}

	consAddr, err := val.GetConsAddr()
	if err != nil {
		return fmt.Errorf("failed to get consensus address: %w", err)
	}

	if err := k.stakingKeeper.Jail(ctx, sdk.ConsAddress(consAddr)); err != nil {
		return fmt.Errorf("failed to jail validator: %w", err)
	}

	sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTLEValidatorJailed,
		sdk.NewAttribute(types.AttributeValidator, validatorAddr),
		sdk.NewAttribute(types.AttributeMissedCount, fmt.Sprintf("%d", missedCount)),
	))

	return nil
}

// pruneTleParticipation removes TleEpochParticipation records that are
// older than the miss window.
func (k Keeper) pruneTleParticipation(ctx context.Context, currentEpoch uint64, params types.Params) error {
	window := uint64(params.TleMissWindow)
	if window == 0 || currentEpoch <= window {
		return nil // nothing to prune
	}
	pruneBelow := currentEpoch - window

	// Walk and collect keys to remove (cannot remove during Walk).
	var toRemove []uint64
	err := k.TleEpochParticipation.Walk(ctx, nil, func(epoch uint64, _ types.TleEpochParticipation) (bool, error) {
		if epoch < pruneBelow {
			toRemove = append(toRemove, epoch)
		}
		return false, nil
	})
	if err != nil {
		return err
	}

	for _, epoch := range toRemove {
		if err := k.TleEpochParticipation.Remove(ctx, epoch); err != nil {
			return err
		}
	}

	return nil
}

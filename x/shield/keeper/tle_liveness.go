package keeper

import (
	"context"
	"fmt"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/shield/types"
)

// checkTLELiveness checks whether TLE validators submitted their decryption shares
// for the completed epoch. Uses a sliding window ring buffer (similar to x/slashing's
// signed_blocks_window) to track participation over the last TleMissWindow epochs.
//
// Called from EndBlocker at each epoch boundary, after batch processing.
func (k Keeper) checkTLELiveness(ctx context.Context, completedEpoch uint64) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	params, err := k.Params.Get(ctx)
	if err != nil {
		return
	}

	if params.TleMissWindow == 0 || params.TleMissTolerance == 0 {
		return
	}

	// Get the TLE key set (DKG must be complete for liveness to matter)
	ks, found := k.GetTLEKeySetVal(ctx)
	if !found || len(ks.MasterPublicKey) == 0 || len(ks.ValidatorShares) == 0 {
		return
	}

	window := params.TleMissWindow
	slot := completedEpoch % window // ring buffer position

	// For each registered TLE validator, record participation and count misses
	for _, vs := range ks.ValidatorShares {
		_, submitted := k.GetDecryptionShare(ctx, completedEpoch, vs.ValidatorAddress)

		// Record participation in the ring buffer at the current slot
		_ = k.TLEEpochParticipation.Set(ctx, collections.Join(vs.ValidatorAddress, slot), submitted)

		if !submitted {
			sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
				types.EventTypeTLEMiss,
				sdk.NewAttribute(types.AttributeKeyValidator, vs.ValidatorAddress),
				sdk.NewAttribute(types.AttributeKeyEpoch, fmt.Sprintf("%d", completedEpoch)),
			))
		}

		// Count misses across the full window
		missCount := k.countTLEMissesInWindow(ctx, vs.ValidatorAddress, window)

		// Update the legacy miss counter for query compatibility
		_ = k.SetTLEMissCount(ctx, vs.ValidatorAddress, missCount)

		if missCount > params.TleMissTolerance {
			k.jailTLEValidator(ctx, params, vs.ValidatorAddress)
			// Clear the ring buffer after jailing to give a fresh start
			k.clearTLEParticipation(ctx, vs.ValidatorAddress, window)
		}
	}
}

// countTLEMissesInWindow counts the number of missed epochs in the sliding window.
func (k Keeper) countTLEMissesInWindow(ctx context.Context, validatorAddr string, window uint64) uint64 {
	var misses uint64
	for slot := uint64(0); slot < window; slot++ {
		participated, err := k.TLEEpochParticipation.Get(ctx, collections.Join(validatorAddr, slot))
		if err != nil {
			// Slot not yet written (epoch hasn't reached this slot) — not a miss
			continue
		}
		if !participated {
			misses++
		}
	}
	return misses
}

// clearTLEParticipation resets the ring buffer for a validator after jailing.
func (k Keeper) clearTLEParticipation(ctx context.Context, validatorAddr string, window uint64) {
	for slot := uint64(0); slot < window; slot++ {
		_ = k.TLEEpochParticipation.Remove(ctx, collections.Join(validatorAddr, slot))
	}
}

// jailTLEValidator jails a validator for TLE liveness failure.
func (k Keeper) jailTLEValidator(ctx context.Context, params types.Params, validatorAddr string) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	if k.late.slashingKeeper == nil || k.late.stakingKeeper == nil {
		return // Not wired — testing/early startup
	}

	// Convert operator address (sprkdrmvaloper1...) to ValAddress
	valAddr, err := sdk.ValAddressFromBech32(validatorAddr)
	if err != nil {
		return
	}

	// Look up the validator to get their consensus address
	validator, err := k.late.stakingKeeper.GetValidator(ctx, valAddr)
	if err != nil {
		return // Validator not found (deregistered?)
	}

	consAddrBytes, err := validator.GetConsAddr()
	if err != nil {
		return
	}
	consAddr := sdk.ConsAddress(consAddrBytes)

	// Jail the validator
	if err := k.late.slashingKeeper.Jail(ctx, consAddr); err != nil {
		return // Jailing failed — log event but don't halt
	}

	// Reset miss counter after jailing
	_ = k.ResetTLEMissCount(ctx, validatorAddr)

	sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeTLEJail,
		sdk.NewAttribute(types.AttributeKeyValidator, validatorAddr),
		sdk.NewAttribute(types.AttributeKeyJailDuration, fmt.Sprintf("%d", params.TleJailDuration)),
	))
}

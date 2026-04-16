package keeper

import (
	"context"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/shield/types"
)

// checkTLELiveness checks whether TLE validators submitted their decryption shares
// for the completed epoch. Validators who missed the deadline get their miss counter
// incremented. If a validator exceeds the tolerance within the miss window, they are jailed.
//
// Called from EndBlocker at each epoch boundary, after batch processing.
func (k Keeper) checkTLELiveness(ctx context.Context, completedEpoch uint64) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	params, err := k.Params.Get(ctx)
	if err != nil {
		return
	}

	// If TLE miss window or tolerance are zero, skip liveness checks
	// TODO: The miss counter is a simple accumulator that resets on any successful participation.
	// This does not implement a true sliding window — a validator who misses (tolerance-1) epochs,
	// participates once (resetting the counter), then misses again will never be jailed.
	// A proper sliding window should track per-epoch participation in a ring buffer
	// (similar to x/slashing's signed_blocks_window) to enforce "at most N misses in M epochs".
	if params.TleMissWindow == 0 || params.TleMissTolerance == 0 {
		return
	}

	// Get the TLE key set (DKG must be complete for liveness to matter)
	ks, found := k.GetTLEKeySetVal(ctx)
	if !found || len(ks.MasterPublicKey) == 0 || len(ks.ValidatorShares) == 0 {
		return
	}

	// For each registered TLE validator, check if they submitted a share for the completed epoch
	for _, vs := range ks.ValidatorShares {
		_, submitted := k.GetDecryptionShare(ctx, completedEpoch, vs.ValidatorAddress)
		if submitted {
			// Validator participated — reset their miss counter
			_ = k.ResetTLEMissCount(ctx, vs.ValidatorAddress)
			continue
		}

		// Validator missed — increment counter
		newCount := k.IncrementTLEMissCount(ctx, vs.ValidatorAddress)

		sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
			types.EventTypeTLEMiss,
			sdk.NewAttribute(types.AttributeKeyValidator, vs.ValidatorAddress),
			sdk.NewAttribute(types.AttributeKeyEpoch, fmt.Sprintf("%d", completedEpoch)),
			sdk.NewAttribute(types.AttributeKeyMissCount, fmt.Sprintf("%d", newCount)),
		))

		// Check if tolerance exceeded
		if newCount > params.TleMissTolerance {
			k.jailTLEValidator(ctx, params, vs.ValidatorAddress)
		}
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

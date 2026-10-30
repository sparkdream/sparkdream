package keeper

import (
	"context"
	"errors"
	"fmt"
	commontypes "sparkdream/x/common/types"
	"time"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/shield/types"
)

// judgeTLELiveness runs every block while batch mode is on and judges every
// demanded epoch that validators have had a full epoch interval to serve: a
// bonded validator in the key set without a stored (verified) share for it
// missed. Idle epochs, and epochs whose batch never became ready, demand
// nothing and count against no one. Each demand is judged once; it stays
// recorded (see demandHeight) so late shares are still accepted until the
// epoch's ops expire. Validators are asked for their share until the demand
// is judged (NextEpochNeedingShare), so nobody is judged for a share the
// chain stopped asking for.
func (k Keeper) judgeTLELiveness(ctx context.Context, params types.Params, currentHeight int64) {
	var due []uint64
	if err := k.IterateDecryptionDemands(ctx, func(epoch uint64, raw int64) bool {
		height, judged := demandHeight(raw)
		if !judged && currentHeight >= height+commontypes.Saturate[int64](params.ShieldEpochInterval) {
			due = append(due, epoch)
		}
		return false
	}); err != nil {
		sdk.UnwrapSDKContext(ctx).Logger().Error("judgeTLELiveness: k.IterateDecryptionDemands failed", "err", err)
	}
	for _, epoch := range due {
		height, _ := k.GetDecryptionDemand(ctx, epoch)
		k.checkTLELiveness(ctx, epoch)
		if err := k.DecryptionDemands.Set(ctx, epoch, -(height + 1)); err != nil {
			sdk.UnwrapSDKContext(ctx).Logger().Error("judgeTLELiveness: DecryptionDemands.Set failed", "err", err)
		}
	}
}

// checkTLELiveness records, for one demanded epoch, which TLE validators
// submitted their decryption share, in a ring buffer over the last
// TleMissWindow *judged* epochs (slot = judged count mod window, so sparse
// demands do not keep a miss alive indefinitely); more than TleMissTolerance
// misses in the ring jails. Validators that are no longer bonded are not
// judged: they cannot vote, so they cannot serve, and jailing them is moot.
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
	judged, getErr := k.TLEJudgedEpochs.Get(ctx)
	if getErr != nil && !errors.Is(getErr, collections.ErrNotFound) {
		sdk.UnwrapSDKContext(ctx).Logger().Error("checkTLELiveness: TLEJudgedEpochs.Get failed", "err", getErr)
	}
	slot := judged % window
	if err := k.TLEJudgedEpochs.Set(ctx, judged+1); err != nil {
		sdk.UnwrapSDKContext(ctx).Logger().Error("checkTLELiveness: TLEJudgedEpochs.Set failed", "err", err)
	}

	// For each registered TLE validator, record participation and count misses
	for _, vs := range ks.ValidatorShares {
		if !k.isBondedValidator(ctx, vs.ValidatorAddress) {
			continue
		}
		_, submitted := k.GetDecryptionShare(ctx, completedEpoch, vs.ValidatorAddress)

		// Record participation in the ring buffer at the current slot
		if err := k.TLEEpochParticipation.Set(ctx, collections.Join(vs.ValidatorAddress, slot), submitted); err != nil {
			sdk.UnwrapSDKContext(ctx).Logger().Error("checkTLELiveness: TLEEpochParticipation.Set failed", "err", err)
		}

		if !submitted {
			sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
				types.EventTypeTLEMiss,
				sdk.NewAttribute(types.AttributeKeyValidator, vs.ValidatorAddress),
				sdk.NewAttribute(types.AttributeKeyEpoch, fmt.Sprintf("%d", completedEpoch)),
			))
		}

		// Count misses across the full window
		missCount := k.countTLEMissesInWindow(ctx, vs.ValidatorAddress, window)

		// Keep the queryable miss counter in step with the ring
		if err := k.SetTLEMissCount(ctx, vs.ValidatorAddress, missCount); err != nil {
			sdk.UnwrapSDKContext(ctx).Logger().Error("checkTLELiveness: k.SetTLEMissCount failed", "err", err)
		}

		if missCount > params.TleMissTolerance {
			k.jailTLEValidator(ctx, params, vs.ValidatorAddress)
			// Clear the ring buffer after jailing to give a fresh start
			k.clearTLEParticipation(ctx, vs.ValidatorAddress, window)
		}
	}
}

// isBondedValidator reports whether the operator is currently in the bonded
// set. Without a staking keeper (unit tests) or with an address the staking
// keeper cannot parse, the validator is judged.
func (k Keeper) isBondedValidator(ctx context.Context, validatorAddr string) bool {
	if k.late.stakingKeeper == nil {
		return true
	}
	valAddr, err := sdk.ValAddressFromBech32(validatorAddr)
	if err != nil {
		return true
	}
	validator, err := k.late.stakingKeeper.GetValidator(ctx, valAddr)
	if err != nil {
		return false
	}
	return validator.IsBonded()
}

// countTLEMissesInWindow counts the number of missed epochs in the sliding window.
func (k Keeper) countTLEMissesInWindow(ctx context.Context, validatorAddr string, window uint64) uint64 {
	var misses uint64
	for slot := uint64(0); slot < window; slot++ {
		participated, err := k.TLEEpochParticipation.Get(ctx, collections.Join(validatorAddr, slot))
		if err != nil {
			// Slot not yet written (fewer than `window` epochs judged) — not a miss
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
		if err := k.TLEEpochParticipation.Remove(ctx, collections.Join(validatorAddr, slot)); err != nil {
			sdk.UnwrapSDKContext(ctx).Logger().Error("clearTLEParticipation: TLEEpochParticipation.Remove failed", "err", err)
		}
	}
}

// ResetTLELiveness forgets every validator's participation ring, miss
// counter and the judged-epoch counter. Called whenever the key set changes:
// misses under a retired key set must not carry into the new one, and
// validators dropped by a re-key would otherwise keep stale entries forever.
func (k Keeper) ResetTLELiveness(ctx context.Context) error {
	iter, err := k.TLEEpochParticipation.Iterate(ctx, nil)
	if err != nil {
		return err
	}
	keys, err := iter.Keys()
	iter.Close()
	if err != nil {
		return err
	}
	for _, key := range keys {
		if err := k.TLEEpochParticipation.Remove(ctx, key); err != nil {
			return err
		}
	}
	var vals []string
	if err := k.IterateTLEMissCounters(ctx, func(addr string, _ uint64) bool {
		vals = append(vals, addr)
		return false
	}); err != nil {
		return err
	}
	for _, v := range vals {
		if err := k.TLEMissCounters.Remove(ctx, v); err != nil {
			return err
		}
	}
	return k.TLEJudgedEpochs.Remove(ctx)
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
	// tle_jail_duration: MsgUnjail is refused until this time passes.
	if params.TleJailDuration > 0 {
		until := sdkCtx.BlockTime().Add(time.Duration(params.TleJailDuration) * time.Second)
		if err := k.late.slashingKeeper.JailUntil(ctx, consAddr, until); err != nil {
			sdk.UnwrapSDKContext(ctx).Logger().Error("jailTLEValidator: late.slashingKeeper.JailUntil failed", "err", err)
		}
	}

	// Reset miss counter after jailing
	if err := k.ResetTLEMissCount(ctx, validatorAddr); err != nil {
		sdk.UnwrapSDKContext(ctx).Logger().Error("jailTLEValidator: k.ResetTLEMissCount failed", "err", err)
	}

	sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
		types.EventTypeTLEJail,
		sdk.NewAttribute(types.AttributeKeyValidator, validatorAddr),
		sdk.NewAttribute(types.AttributeKeyJailDuration, fmt.Sprintf("%d", params.TleJailDuration)),
	))
}

package keeper

import (
	"context"
	"time"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/commons/types"
)

// CheckSpendPreconditions validates that `authority` (a council policy
// address) is currently allowed to disburse `amount` and, if so, atomically
// records the spend against its per-epoch budget.
//
// The checks are:
//  1. authority maps to a registered group;
//  2. the group is past its activation time (i.e. not a shell);
//  3. the group's current term has not expired (i.e. not a zombie);
//  4. the spend respects the group's `max_spend_per_epoch` (both single-tx
//     and cumulative-over-the-epoch).
//
// On success the EpochSpending entry for (authority, epochDay) is updated to
// include this disbursement, so subsequent calls within the same epoch see
// the cumulative total.
//
// **Layering (M4):** the body is now factored into `checkSpendGates`
// (the four checks, no writes) + `recordEpochSpend` (the per-epoch
// debit). MsgSpendFromCommons keeps calling the combined wrapper.
// SessionClaimHook uses the split: PreCheck → checkSpendGates,
// PostCommit → recordEpochSpend. The split is what prevents the
// double-debit bug documented in the migration plan §3.2.
//
// Callers are still responsible for the actual `bankKeeper.SendCoins`.
func (k Keeper) CheckSpendPreconditions(ctx context.Context, authority string, amount sdk.Coins) error {
	if err := k.checkSpendGates(ctx, authority, amount); err != nil {
		return err
	}
	return k.recordEpochSpend(ctx, authority, amount)
}

// checkSpendGates runs steps 1–4a of the precondition check (group
// lookup, activation, term-expiry, per-epoch CHECK with no write). A
// non-nil return indicates the disbursement is not currently allowed.
//
// Used as the PreCheck body of the SessionClaimHook (M4).
func (k Keeper) checkSpendGates(ctx context.Context, authority string, amount sdk.Coins) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// 1. Lookup the group via its policy address.
	_, extGroup, found := k.GetGroupByPolicy(sdkCtx, authority)
	if !found {
		return errorsmod.Wrapf(types.ErrGroupNotFound,
			"signer %s is not a registered group policy", authority)
	}

	// 2. Activation gate (Shell groups).
	if extGroup.ActivationTime > 0 && sdkCtx.BlockTime().Unix() < extGroup.ActivationTime {
		activationTime := time.Unix(extGroup.ActivationTime, 0)
		return errorsmod.Wrapf(types.ErrGroupNotActive,
			"group is in pre-launch phase; active from %s", activationTime.String())
	}

	// 3. Term-expiration gate (Zombie groups). A recurring schedule that
	// outlives its authorizing council's term auto-pauses here; on term
	// renewal the same code path passes again and claims resume.
	if extGroup.CurrentTermExpiration > 0 && sdkCtx.BlockTime().Unix() > extGroup.CurrentTermExpiration {
		expirationTime := time.Unix(extGroup.CurrentTermExpiration, 0)
		return errorsmod.Wrapf(types.ErrGroupExpired,
			"group term ended on %s; parent must renew membership", expirationTime.String())
	}

	// 4a. Per-epoch rate limit CHECK only (no write to EpochSpending).
	// recordEpochSpend below performs the corresponding write.
	if extGroup.MaxSpendPerEpoch != nil && extGroup.MaxSpendPerEpoch.GT(math.NewInt(0)) {
		limit := *extGroup.MaxSpendPerEpoch
		epochDay := sdkCtx.BlockTime().Unix() / 86400

		requestedUspark := amount.AmountOf(k.BondDenom(ctx))

		// Single-transaction check.
		if requestedUspark.GT(limit) {
			return errorsmod.Wrapf(types.ErrRateLimitExceeded,
				"spend request %s uspark exceeds epoch limit of %s uspark", requestedUspark, limit)
		}

		// Cumulative-over-epoch check.
		key := collections.Join(authority, epochDay)
		cumulativeSpent := math.ZeroInt()
		if prev, err := k.EpochSpending.Get(ctx, key); err == nil {
			var ok bool
			cumulativeSpent, ok = math.NewIntFromString(prev)
			if !ok {
				cumulativeSpent = math.ZeroInt()
			}
		}

		newTotal := cumulativeSpent.Add(requestedUspark)
		if newTotal.GT(limit) {
			return errorsmod.Wrapf(types.ErrRateLimitExceeded,
				"cumulative spend this epoch %s + request %s = %s exceeds limit %s uspark",
				cumulativeSpent, requestedUspark, newTotal, limit)
		}
	}

	return nil
}

// recordEpochSpend commits the per-epoch bucket write that step 4b
// of CheckSpendPreconditions used to perform inline. Idempotent in
// the sense that the caller is responsible for not calling it twice
// for the same disbursement; the SessionClaimHook contract pairs it
// with checkSpendGates on the same (authority, amount).
//
// Used as the PostCommit body of the SessionClaimHook (M4). PostCommit
// errors are tx-halting per the session hook contract; a write
// failure here will roll back the entire claim tx so a debited budget
// always corresponds to a successful disbursement.
//
// No-op if the council has no max_spend_per_epoch configured.
func (k Keeper) recordEpochSpend(ctx context.Context, authority string, amount sdk.Coins) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Re-fetch group; checkSpendGates already validated it exists.
	_, extGroup, found := k.GetGroupByPolicy(sdkCtx, authority)
	if !found {
		// Should be unreachable when called paired with checkSpendGates,
		// but be defensive.
		return errorsmod.Wrapf(types.ErrGroupNotFound,
			"signer %s is not a registered group policy", authority)
	}

	if extGroup.MaxSpendPerEpoch == nil || !extGroup.MaxSpendPerEpoch.GT(math.NewInt(0)) {
		// No per-epoch cap configured: nothing to debit.
		return nil
	}

	epochDay := sdkCtx.BlockTime().Unix() / 86400
	requestedUspark := amount.AmountOf(k.BondDenom(ctx))

	key := collections.Join(authority, epochDay)
	cumulativeSpent := math.ZeroInt()
	if prev, err := k.EpochSpending.Get(ctx, key); err == nil {
		var ok bool
		cumulativeSpent, ok = math.NewIntFromString(prev)
		if !ok {
			cumulativeSpent = math.ZeroInt()
		}
	}

	newTotal := cumulativeSpent.Add(requestedUspark)
	if err := k.EpochSpending.Set(ctx, key, newTotal.String()); err != nil {
		return errorsmod.Wrap(err, "failed to update epoch spending tracker")
	}
	return nil
}

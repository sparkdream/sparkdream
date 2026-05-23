package keeper

import (
	"context"
	"errors"
	"fmt"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/session/types"
)

// ClaimRecurringPull disburses one period of a RECURRING_PULL grant
// from the granter to the grantee.
//
// Each claim:
//   - verifies the grant exists, is RECURRING_PULL, and is ACTIVE,
//   - verifies the grant's logical clock is at least one period in the
//     past relative to block time (catch-up requires multiple txs),
//   - debits the current UTC-day bucket and rejects if it would breach
//     max_per_epoch,
//   - transfers amount_per_period from granter to grantee,
//   - advances last_claim_advance by exactly period_seconds (anchored to
//     the grant, not block time), and
//   - flips status to COMPLETED if the next advance would exceed
//     expires_at.
//
// On insufficient granter balance the grant transitions to
// PAUSED_INSUFFICIENT_FUNDS; subsequent calls re-attempt and flip back to
// ACTIVE on the first success. The pause is observable via the
// grant_paused_underfunded / grant_resumed events.
//
// Body factored into Keeper.claimRecurringPullCommon so the privileged
// keeper-side entrypoint `ClaimRecurringPullForGrantee` (used by the
// x/commons RecurringSpend wrapper) shares the exact same period
// check, hook-invocation, bank send, and status-transition logic.
func (k msgServer) ClaimRecurringPull(ctx context.Context, msg *types.MsgClaimRecurringPull) (*types.MsgClaimRecurringPullResponse, error) {
	return k.Keeper.claimRecurringPullCommon(ctx, msg.GrantId, msg.Grantee)
}

// claimRecurringPullCommon is the shared implementation behind both
// the msg-server `ClaimRecurringPull` and the privileged keeper-side
// `ClaimRecurringPullForGrantee`. It assumes the caller has already
// performed any outer signer / allowlist authorization; this function
// is responsible for grant-level validation (type, grantee match,
// status, period), the epoch-bucket gate, the bank send + pause/resume
// transition, the hook PreCheck + PostCommit passes, and event
// emission.
func (k Keeper) claimRecurringPullCommon(ctx context.Context, grantID uint64, grantee string) (*types.MsgClaimRecurringPullResponse, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	grant, err := k.Grants.Get(ctx, grantID)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, errorsmod.Wrapf(types.ErrGrantNotFound, "id=%d", grantID)
		}
		return nil, err
	}

	if grant.Type != types.GrantType_GRANT_TYPE_RECURRING_PULL {
		return nil, errorsmod.Wrapf(types.ErrGrantTypeMismatch,
			"grant %d is %s, expected RECURRING_PULL", grant.Id, grant.Type)
	}
	rp := grant.GetRecurringPull()
	if rp == nil {
		return nil, errorsmod.Wrap(types.ErrInvalidPayload, "recurring_pull payload missing")
	}

	if grant.Grantee != grantee {
		return nil, errorsmod.Wrapf(types.ErrRecurringPullUnauthorized,
			"caller %s is not the grantee %s", grantee, grant.Grantee)
	}

	// Status must be ACTIVE or PAUSED_INSUFFICIENT_FUNDS (a paused grant
	// is recoverable in-line on the next successful retry).
	switch grant.Status {
	case types.GrantStatus_GRANT_STATUS_ACTIVE,
		types.GrantStatus_GRANT_STATUS_PAUSED_INSUFFICIENT_FUNDS:
	default:
		return nil, errorsmod.Wrapf(types.ErrGrantInactive,
			"grant %d is in status %s", grant.Id, grant.Status)
	}

	now := sdkCtx.BlockTime().Unix()
	nextEligible := rp.LastClaimAdvance + rp.PeriodSeconds
	if now < nextEligible {
		return nil, errorsmod.Wrapf(types.ErrRecurringPullNotDue,
			"next claim window opens at %d (now=%d)", nextEligible, now)
	}
	if nextEligible > grant.ExpiresAt.Unix() {
		// Window closed; flip to COMPLETED so future calls short-circuit.
		grant.Status = types.GrantStatus_GRANT_STATUS_COMPLETED
		grant.Payload = &types.Grant_RecurringPull{RecurringPull: rp}
		if err := k.Grants.Set(ctx, grant.Id, grant); err != nil {
			return nil, err
		}
		if err := k.decActiveGrantCount(ctx, grant.Granter, grant.Type); err != nil {
			return nil, err
		}
		return nil, errorsmod.Wrapf(types.ErrRecurringPullWindowClosed,
			"grant %d ended at %d", grant.Id, grant.ExpiresAt.Unix())
	}

	// Epoch self-throttle: debit the current UTC-day bucket. Reject if
	// it would breach max_per_epoch.
	day := utcDayIndex(sdkCtx.BlockTime())
	maxPerEpoch, ok := sdkmath.NewIntFromString(rp.MaxPerEpoch)
	if !ok {
		// Should be impossible — validated at creation. Treat as zero.
		maxPerEpoch = sdkmath.ZeroInt()
	}
	// Drop stale buckets opportunistically.
	k.pruneStaleEpochBuckets(ctx, grant.Id, day)

	// Pre-check: if adding amount would breach the per-epoch cap, reject
	// before touching bank state.
	if newBucket, err := k.previewEpochSpend(ctx, grant.Id, day, rp.AmountPerPeriod.Amount, maxPerEpoch); err != nil {
		_ = newBucket
		return nil, err
	}

	// Grant-claim hooks — PreCheck pass. Downstream modules can veto the
	// claim here. Used by the x/commons RecurringSpend migration to
	// apply group activation / term / rate-limit checks for
	// council-policy-owned grants. No-op when no hooks are registered.
	if err := k.invokePreCheckHooks(ctx, grant, sdk.NewCoins(rp.AmountPerPeriod)); err != nil {
		return nil, err
	}

	// Bank send. If the granter is underfunded, flip to PAUSED and bail
	// out. If the granter recovers later, the next claim's bank send
	// succeeds and we flip back to ACTIVE atomically (below).
	granterAddr, err := k.addressCodec.StringToBytes(grant.Granter)
	if err != nil {
		return nil, err
	}
	granteeAddr, err := k.addressCodec.StringToBytes(grant.Grantee)
	if err != nil {
		return nil, err
	}
	if err := k.bankKeeper.SendCoins(ctx, granterAddr, granteeAddr, sdk.NewCoins(rp.AmountPerPeriod)); err != nil {
		// Treat any send failure as insufficient funds for the pause
		// transition. Re-classifying based on err type is plan P4+ work.
		previousStatus := grant.Status
		grant.Status = types.GrantStatus_GRANT_STATUS_PAUSED_INSUFFICIENT_FUNDS
		grant.Payload = &types.Grant_RecurringPull{RecurringPull: rp}
		if persistErr := k.Grants.Set(ctx, grant.Id, grant); persistErr != nil {
			return nil, persistErr
		}
		if previousStatus == types.GrantStatus_GRANT_STATUS_ACTIVE {
			sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
				"grant_paused_underfunded",
				sdk.NewAttribute("id", fmt.Sprintf("%d", grant.Id)),
				sdk.NewAttribute("type", grant.Type.String()),
				sdk.NewAttribute("granter", grant.Granter),
				sdk.NewAttribute("grantee", grant.Grantee),
				sdk.NewAttribute("attempted_amount", rp.AmountPerPeriod.String()),
			))
		}
		return nil, errorsmod.Wrap(types.ErrInsufficientGranterBalance, err.Error())
	}

	// Bank send succeeded — commit the epoch-bucket debit.
	if _, err := k.addEpochSpend(ctx, grant.Id, day, rp.AmountPerPeriod.Amount, maxPerEpoch); err != nil {
		// Should be unreachable given the previewEpochSpend gate above;
		// keep as defense-in-depth.
		return nil, err
	}

	wasPaused := grant.Status == types.GrantStatus_GRANT_STATUS_PAUSED_INSUFFICIENT_FUNDS

	rp.LastClaimAdvance = nextEligible
	rp.ClaimsMade++
	grant.Payload = &types.Grant_RecurringPull{RecurringPull: rp}
	grant.Status = types.GrantStatus_GRANT_STATUS_ACTIVE

	completed := false
	if rp.LastClaimAdvance+rp.PeriodSeconds > grant.ExpiresAt.Unix() {
		grant.Status = types.GrantStatus_GRANT_STATUS_COMPLETED
		completed = true
	}

	if err := k.Grants.Set(ctx, grant.Id, grant); err != nil {
		return nil, err
	}
	if completed {
		if err := k.decActiveGrantCount(ctx, grant.Granter, grant.Type); err != nil {
			return nil, err
		}
	}

	// Grant-claim hooks — PostCommit pass. State-mutating side effects
	// (e.g. x/commons per-epoch budget debit) live here so they are
	// atomic with the disbursement. A non-nil error halts the tx and
	// the SDK rolls back the bank send, the grant update, and any state
	// writes done since PreCheck.
	if err := k.invokePostCommitHooks(ctx, grant, sdk.NewCoins(rp.AmountPerPeriod)); err != nil {
		return nil, err
	}

	if wasPaused && !completed {
		sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
			"grant_resumed",
			sdk.NewAttribute("id", fmt.Sprintf("%d", grant.Id)),
			sdk.NewAttribute("type", grant.Type.String()),
			sdk.NewAttribute("granter", grant.Granter),
			sdk.NewAttribute("grantee", grant.Grantee),
		))
	}

	sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
		"recurring_pull_claimed",
		sdk.NewAttribute("grant_id", fmt.Sprintf("%d", grant.Id)),
		sdk.NewAttribute("granter", grant.Granter),
		sdk.NewAttribute("grantee", grant.Grantee),
		sdk.NewAttribute("amount", rp.AmountPerPeriod.String()),
		sdk.NewAttribute("claim_index", fmt.Sprintf("%d", rp.ClaimsMade)),
	))

	return &types.MsgClaimRecurringPullResponse{
		ClaimNumber:   rp.ClaimsMade,
		NextClaimTime: rp.LastClaimAdvance + rp.PeriodSeconds,
	}, nil
}

// previewEpochSpend reports whether (cur + amount) would breach
// maxPerEpoch without committing the new value. Splits the "check" from
// the "commit" so that a failed bank send doesn't leave the epoch
// bucket prematurely debited.
func (k Keeper) previewEpochSpend(
	ctx context.Context,
	grantID uint64,
	day int64,
	amount sdkmath.Int,
	maxPerEpoch sdkmath.Int,
) (sdkmath.Int, error) {
	key := collections.Join(grantID, day)
	cur := sdkmath.ZeroInt()
	stored, err := k.EpochSpendByGrant.Get(ctx, key)
	if err == nil {
		v, ok := sdkmath.NewIntFromString(stored)
		if ok {
			cur = v
		}
	}
	next := cur.Add(amount)
	if next.GT(maxPerEpoch) {
		return cur, types.ErrEpochCeilingExceeded
	}
	return next, nil
}

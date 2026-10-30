package keeper

import (
	"fmt"
	"time"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/session/types"
)

// fireEntry is the (id, fire_at) tuple sorted by firePass before
// dispatch. Hoisted to package scope so sortFireEntries can take it as
// a slice argument (the inline struct literal didn't unify).
type fireEntry struct {
	id     uint64
	fireAt int64
}

const maxPrunePerBlock = 100

// EndBlocker runs three ordered, independently-capped passes per Rev 2 §6:
//
//  1. Fire scheduled oneshots whose `fire_at <= block_time`. Strict
//     `(fire_at ASC, grant_id ASC)` ordering for deterministic replay.
//  2. Auto-revoke paused oneshots older than params.paused_oneshot_ttl_seconds;
//     refund the held deposit to the granter.
//  3. Expire grants whose `expires_at <= block_time`. Status -> COMPLETED
//     (or REVOKED for oneshots that have a deposit refund implied).
//
// Each pass caps at params.max_endblocker_dispatches_per_pass. If pass 1
// fills its cap, passes 2 and 3 still run with their own caps.
func (k Keeper) EndBlocker(ctx sdk.Context) error {
	params, err := k.Params.Get(ctx)
	if err != nil {
		return err
	}
	cap := int(params.MaxEndblockerDispatchesPerPass)
	if cap == 0 {
		cap = maxPrunePerBlock
	}

	blockTime := ctx.BlockTime()

	if err := k.firePass(ctx, blockTime, cap); err != nil {
		return err
	}
	if err := k.autoRevokePausedPass(ctx, blockTime, params.PausedOneshotTtlSeconds, cap); err != nil {
		return err
	}
	if err := k.expirePass(ctx, blockTime, cap); err != nil {
		return err
	}
	return nil
}

// firePass fires ScheduledOneshot grants whose fire_at <= block_time in
// (fire_at ASC, id ASC) order via fireScheduledOneshot, capped at `cap`
// dispatches per block — backlog drains over subsequent blocks per Rev 2
// §4.4.4(e).
//
// It range-walks the GrantsByFireTime index, which holds only ACTIVE
// oneshots, so per-block work is bounded by the cap and never by the
// number of grants in the store (SES-02). The index key order is the
// dispatch order. An entry whose grant is gone, inactive or rescheduled
// is stale: it is dropped (and counted against the cap) without firing.
func (k Keeper) firePass(ctx sdk.Context, blockTime time.Time, cap int) error {
	var due []fireEntry
	rng := new(collections.Range[collections.Pair[int64, uint64]]).
		EndExclusive(collections.Join[int64, uint64](blockTime.Unix()+1, 0))
	err := k.GrantsByFireTime.Walk(ctx, rng, func(key collections.Pair[int64, uint64]) (bool, error) {
		if len(due) >= cap {
			return true, nil
		}
		due = append(due, fireEntry{id: key.K2(), fireAt: key.K1()})
		return false, nil
	})
	if err != nil {
		return err
	}

	for _, e := range due {
		g, err := k.Grants.Get(ctx, e.id)
		so := g.GetScheduledOneshot()
		if err != nil || g.Status != types.GrantStatus_GRANT_STATUS_ACTIVE || so == nil || so.FireAt != e.fireAt {
			if err := k.GrantsByFireTimeRemove(ctx, e.fireAt, e.id); err != nil {
				sdk.UnwrapSDKContext(ctx).Logger().Error("firePass: k.GrantsByFireTimeRemove failed", "err", err)
			}
			continue
		}
		if err := k.fireScheduledOneshot(ctx, e.id); err != nil {
			// fireScheduledOneshot returns nil on contained handler
			// failures (those are FIRED-with-error). A non-nil here
			// means a real persistence error — log and continue so one
			// bad grant doesn't stall the whole pass.
			ctx.Logger().Error("oneshot fire error", "grant_id", e.id, "err", err)
		}
	}
	return nil
}

// autoRevokePausedPass auto-revokes paused oneshots whose pause has
// exceeded ttlSeconds. Refunds the held deposit to the granter.
//
// Walks PausedOneshotByPauseTime[(pause_unix, id) : pause_unix + ttl <=
// block_time], so the TTL counts from the pause (written by
// pauseUnderfundedOneshot), never from creation: a oneshot scheduled
// weeks out still gets the full retry window after it pauses. An entry
// whose grant is gone, no longer paused, or re-paused at a different
// time is stale and is dropped without touching the grant.
func (k Keeper) autoRevokePausedPass(ctx sdk.Context, blockTime time.Time, ttlSeconds int64, cap int) error {
	type entry struct {
		pauseUnix int64
		id        uint64
	}
	var stale []entry

	cutoff := blockTime.Unix() - ttlSeconds
	rng := new(collections.Range[collections.Pair[int64, uint64]]).
		EndExclusive(collections.Join[int64, uint64](cutoff+1, 0))
	err := k.PausedOneshotByPauseTime.Walk(ctx, rng, func(key collections.Pair[int64, uint64]) (bool, error) {
		if len(stale) >= cap {
			return true, nil
		}
		stale = append(stale, entry{pauseUnix: key.K1(), id: key.K2()})
		return false, nil
	})
	if err != nil {
		return err
	}

	for _, e := range stale {
		indexKey := collections.Join(e.pauseUnix, e.id)
		grant, err := k.Grants.Get(ctx, e.id)
		if err != nil {
			if err := k.PausedOneshotByPauseTime.Remove(ctx, indexKey); err != nil {
				ctx.Logger().Error("autoRevokePausedPass: PausedOneshotByPauseTime.Remove failed", "err", err)
			}
			continue
		}
		so := grant.GetScheduledOneshot()
		if grant.Status != types.GrantStatus_GRANT_STATUS_PAUSED_INSUFFICIENT_FUNDS ||
			so == nil || so.FireAt != e.pauseUnix {
			if err := k.PausedOneshotByPauseTime.Remove(ctx, indexKey); err != nil {
				ctx.Logger().Error("autoRevokePausedPass: PausedOneshotByPauseTime.Remove failed", "err", err)
			}
			continue
		}

		// Refund the deposit.
		depositCoin, depErr := k.OneshotGasDeposit.Get(ctx, e.id)
		if depErr == nil && !depositCoin.IsZero() {
			k.settleDeposit(ctx, grant, depositCoin)
		}

		// deleteGrant sees the PAUSED status, so it releases the
		// per-(granter, type) active slot the paused grant was pinning,
		// and clears every index including the paused-TTL entry.
		if err := k.deleteGrant(ctx, grant); err != nil {
			ctx.Logger().Error("paused oneshot auto-revoke failed",
				"grant_id", e.id, "err", err)
			continue
		}

		ctx.EventManager().EmitEvent(sdk.NewEvent(
			"grant_auto_revoked",
			sdk.NewAttribute("id", fmt.Sprintf("%d", grant.Id)),
			sdk.NewAttribute("granter", grant.Granter),
			sdk.NewAttribute("grantee", grant.Grantee),
			sdk.NewAttribute("refund_amount", depositCoin.String()),
		))
	}
	return nil
}

// expirePass walks GrantsByExpiration[<= block_time] and removes
// expired grants, emitting per-type events. Caps at `cap` per block.
// Refunds any held oneshot deposit on expire (treats expire-without-
// fire as a granter-side cancel).
func (k Keeper) expirePass(ctx sdk.Context, blockTime time.Time, cap int) error {
	pruned := 0
	rng := new(collections.Range[collections.Pair[int64, uint64]]).
		EndExclusive(collections.Join[int64, uint64](blockTime.Unix()+1, 0))

	return k.GrantsByExpiration.Walk(ctx, rng, func(key collections.Pair[int64, uint64]) (bool, error) {
		if pruned >= cap {
			return true, nil
		}

		id := key.K2()
		grant, err := k.Grants.Get(ctx, id)
		if err != nil {
			ctx.Logger().Debug("session pruner: stale expiration index entry",
				"id", id, "expiration", key.K1(), "err", err)
			if err := k.GrantsByExpiration.Remove(ctx, key); err != nil {
				ctx.Logger().Error("expirePass: GrantsByExpiration.Remove failed", "err", err)
			}
			pruned++
			return false, nil
		}

		// For ScheduledOneshot grants expiring without firing, refund
		// the deposit to the granter.
		if grant.Type == types.GrantType_GRANT_TYPE_SCHEDULED_ONESHOT {
			if depositCoin, depErr := k.OneshotGasDeposit.Get(ctx, id); depErr == nil && !depositCoin.IsZero() {
				k.settleDeposit(ctx, grant, depositCoin)
			}
		}

		if err := k.deleteGrant(ctx, grant); err != nil {
			return true, err
		}

		switch sk := grant.Payload.(type) {
		case *types.Grant_SessionKey:
			ctx.EventManager().EmitEvent(sdk.NewEvent(
				"session_expired",
				sdk.NewAttribute("granter", grant.Granter),
				sdk.NewAttribute("grantee", grant.Grantee),
				sdk.NewAttribute("exec_count", fmt.Sprintf("%d", sk.SessionKey.ExecCount)),
				sdk.NewAttribute("spent", sk.SessionKey.Spent.String()),
				sdk.NewAttribute("grant_id", fmt.Sprintf("%d", grant.Id)),
			))
		default:
			ctx.EventManager().EmitEvent(sdk.NewEvent(
				"grant_expired",
				sdk.NewAttribute("granter", grant.Granter),
				sdk.NewAttribute("grantee", grant.Grantee),
				sdk.NewAttribute("type", types.GrantTypeEventValue(grant.Type)),
				sdk.NewAttribute("grant_id", fmt.Sprintf("%d", grant.Id)),
			))
		}

		pruned++
		return false, nil
	})
}

// settleDeposit returns an escrowed oneshot deposit to the granter on a
// pass that deletes the grant. If the refund cannot be paid (e.g. the
// granter is a blocked module account) the deposit goes to the fee
// collector instead, so the grant is never deleted with an escrow entry
// left behind that nothing could ever claim. The entry is removed either
// way.
func (k Keeper) settleDeposit(ctx sdk.Context, grant types.Grant, dep sdk.Coin) {
	if err := k.refundOneshotDeposit(ctx, grant.Granter, dep); err != nil {
		ctx.Logger().Error("oneshot deposit refund failed; sending to fee collector",
			"grant_id", grant.Id, "err", err)
		if err := k.bankKeeper.SendCoinsFromModuleToModule(ctx, types.ModuleName, feeCollectorModule, sdk.NewCoins(dep)); err != nil {
			ctx.Logger().Error("oneshot deposit settlement failed", "grant_id", grant.Id, "err", err)
		}
	}
	if err := k.OneshotGasDeposit.Remove(ctx, grant.Id); err != nil {
		ctx.Logger().Error("settleDeposit: OneshotGasDeposit.Remove failed", "err", err)
	}
}

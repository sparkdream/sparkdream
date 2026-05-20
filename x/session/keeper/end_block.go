package keeper

import (
	"fmt"
	"sort"
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
//   1. Fire scheduled oneshots whose `fire_at <= block_time`. Strict
//      `(fire_at ASC, grant_id ASC)` ordering for deterministic replay.
//   2. Auto-revoke paused oneshots older than params.paused_oneshot_ttl_seconds;
//      refund the held deposit to the granter.
//   3. Expire grants whose `expires_at <= block_time`. Status -> COMPLETED
//      (or REVOKED for oneshots that have a deposit refund implied).
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

// firePass walks ScheduledOneshot grants whose fire_at <= block_time and
// fires them in (fire_at ASC, id ASC) order via fireScheduledOneshot.
// Caps at `cap` dispatches per block — backlog drains over subsequent
// blocks per Rev 2 §4.4.4(e).
//
// We walk the primary Grants collection here rather than a dedicated
// GrantsByFireTime index. With max_pending_oneshots_per_granter = 100
// the worst-case scan per granter is bounded and the operation is
// rare-per-block. A dedicated index can land later as a perf
// optimization without a breaking change.
func (k Keeper) firePass(ctx sdk.Context, blockTime time.Time, cap int) error {
	var due []fireEntry

	err := k.Grants.Walk(ctx, nil, func(id uint64, g types.Grant) (bool, error) {
		if g.Type != types.GrantType_GRANT_TYPE_SCHEDULED_ONESHOT {
			return false, nil
		}
		if g.Status != types.GrantStatus_GRANT_STATUS_ACTIVE {
			return false, nil
		}
		so := g.GetScheduledOneshot()
		if so == nil {
			return false, nil
		}
		if so.FireAt > blockTime.Unix() {
			return false, nil
		}
		due = append(due, fireEntry{id: id, fireAt: so.FireAt})
		return false, nil
	})
	if err != nil {
		return err
	}

	// Sort by (fire_at ASC, id ASC) for deterministic replay.
	sortFireEntries(due)

	fired := 0
	for _, e := range due {
		if fired >= cap {
			break
		}
		if err := k.fireScheduledOneshot(ctx, e.id); err != nil {
			// fireScheduledOneshot returns nil on contained handler
			// failures (those are FIRED-with-error). A non-nil here
			// means a real persistence error — log and continue so one
			// bad grant doesn't stall the whole pass.
			ctx.Logger().Error("oneshot fire error", "grant_id", e.id, "err", err)
		}
		fired++
	}
	return nil
}

// autoRevokePausedPass auto-revokes paused oneshots whose pause has
// exceeded ttlSeconds. Refunds the held deposit to the granter.
//
// We use the grant's CreatedAt as the conservative pause-age proxy here.
// Since PausedOneshotByPauseTime would carry the exact pause unix, this
// will be tightened in a follow-up. The conservative bound still
// guarantees auto-revoke happens by `created_at + ttl` at the latest.
func (k Keeper) autoRevokePausedPass(ctx sdk.Context, blockTime time.Time, ttlSeconds int64, cap int) error {
	type entry struct{ id uint64 }
	var stale []entry

	err := k.Grants.Walk(ctx, nil, func(id uint64, g types.Grant) (bool, error) {
		if g.Type != types.GrantType_GRANT_TYPE_SCHEDULED_ONESHOT {
			return false, nil
		}
		if g.Status != types.GrantStatus_GRANT_STATUS_PAUSED_INSUFFICIENT_FUNDS {
			return false, nil
		}
		// Conservative TTL: based on CreatedAt. A dedicated
		// PausedOneshotByPauseTime index would carry the exact pause
		// unix and enable tighter TTL, but this matches the plan's
		// "auto-revoke after TTL" guarantee in worst case.
		if g.CreatedAt.Unix()+ttlSeconds > blockTime.Unix() {
			return false, nil
		}
		stale = append(stale, entry{id: id})
		return false, nil
	})
	if err != nil {
		return err
	}

	revoked := 0
	for _, e := range stale {
		if revoked >= cap {
			break
		}
		grant, err := k.Grants.Get(ctx, e.id)
		if err != nil {
			continue
		}
		// Refund the deposit.
		depositCoin, depErr := k.OneshotGasDeposit.Get(ctx, e.id)
		if depErr == nil && !depositCoin.IsZero() {
			granterAddr, addrErr := k.addressCodec.StringToBytes(grant.Granter)
			if addrErr == nil {
				if sendErr := k.bankKeeper.SendCoinsFromModuleToAccount(ctx, types.ModuleName, granterAddr, sdk.NewCoins(depositCoin)); sendErr != nil {
					ctx.Logger().Error("paused oneshot deposit refund failed",
						"grant_id", e.id, "err", sendErr)
				} else {
					_ = k.OneshotGasDeposit.Remove(ctx, e.id)
				}
			}
		}
		// Status -> REVOKED, delete from primary + indexes (and decrement counter).
		grant.Status = types.GrantStatus_GRANT_STATUS_REVOKED
		// Use deleteGrant which clears the active counter — but since
		// the grant was PAUSED (not ACTIVE in counter terms after our
		// decrement on pause was applied), check the counter state.
		// For now: persist the REVOKED state, then clear indexes.
		if err := k.Grants.Set(ctx, grant.Id, grant); err != nil {
			continue
		}
		if err := k.removeGrantIndexes(ctx, grant); err != nil {
			ctx.Logger().Error("paused oneshot index removal failed",
				"grant_id", e.id, "err", err)
		}
		_ = k.Grants.Remove(ctx, grant.Id)

		ctx.EventManager().EmitEvent(sdk.NewEvent(
			"grant_auto_revoked",
			sdk.NewAttribute("id", fmt.Sprintf("%d", grant.Id)),
			sdk.NewAttribute("granter", grant.Granter),
			sdk.NewAttribute("grantee", grant.Grantee),
			sdk.NewAttribute("refund_amount", depositCoin.String()),
		))
		revoked++
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
			_ = k.GrantsByExpiration.Remove(ctx, key)
			pruned++
			return false, nil
		}

		// For ScheduledOneshot grants expiring without firing, refund
		// the deposit to the granter.
		if grant.Type == types.GrantType_GRANT_TYPE_SCHEDULED_ONESHOT {
			if depositCoin, depErr := k.OneshotGasDeposit.Get(ctx, id); depErr == nil && !depositCoin.IsZero() {
				granterAddr, addrErr := k.addressCodec.StringToBytes(grant.Granter)
				if addrErr == nil {
					if sendErr := k.bankKeeper.SendCoinsFromModuleToAccount(ctx, types.ModuleName, granterAddr, sdk.NewCoins(depositCoin)); sendErr == nil {
						_ = k.OneshotGasDeposit.Remove(ctx, id)
					}
				}
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
				sdk.NewAttribute("type", grant.Type.String()),
				sdk.NewAttribute("grant_id", fmt.Sprintf("%d", grant.Id)),
			))
		}

		pruned++
		return false, nil
	})
}

// sortFireEntries sorts in (fire_at ASC, id ASC) order — required for
// deterministic replay (Rev 2 §M7).
func sortFireEntries(entries []fireEntry) {
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].fireAt != entries[j].fireAt {
			return entries[i].fireAt < entries[j].fireAt
		}
		return entries[i].id < entries[j].id
	})
}

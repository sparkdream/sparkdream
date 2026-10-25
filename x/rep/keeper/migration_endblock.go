package keeper

import (
	"context"
	"fmt"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/rep/types"
)

// ProcessMigrations is the EndBlocker entry point of the member key
// migration: four capped passes, each bounded so a backlog drains over the
// following blocks and none of them can halt the chain.
//
//  1. Finalize ACCEPTED migrations whose executes_at has passed, each in a
//     child cache context; a failure marks the migration FAILED and frees the
//     member's pending slot.
//  2. Expire REQUESTED migrations the new address never accepted.
//  3. Tally objection jury reviews past their deadline.
//  4. Burn the deposits of sealed recovery commits past their TTL.
//  5. Activate pending ZK public keys whose delay has elapsed.
func (k Keeper) ProcessMigrations(ctx context.Context) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	now := sdkCtx.BlockTime().Unix()

	if err := k.finalizeDueMigrations(ctx, now, types.MaxMigrationFinalizationsPerBlock); err != nil {
		sdkCtx.Logger().Error("failed to finalize due migrations", "error", err)
	}
	if err := k.expireUnacceptedMigrations(ctx, now, types.MaxMigrationExpiriesPerBlock); err != nil {
		sdkCtx.Logger().Error("failed to expire unaccepted migrations", "error", err)
	}
	if err := k.SweepObjectedMigrations(ctx); err != nil {
		sdkCtx.Logger().Error("failed to sweep objected migrations", "error", err)
	}
	if err := k.ExpireRecoveryCommits(ctx, now, types.MaxMigrationExpiriesPerBlock); err != nil {
		sdkCtx.Logger().Error("failed to expire recovery commits", "error", err)
	}
	if err := k.ActivatePendingZkKeys(ctx, types.MaxZkKeyActivationsPerBlock); err != nil {
		sdkCtx.Logger().Error("failed to activate pending zk keys", "error", err)
	}
}

// finalizeDueMigrations walks MigrationQueue[(executes_at, id) <= now].
func (k Keeper) finalizeDueMigrations(ctx context.Context, now int64, maxN int) error {
	type entry struct {
		at int64
		id uint64
	}
	var due []entry
	rng := new(collections.Range[collections.Pair[int64, uint64]]).
		EndExclusive(collections.Join[int64, uint64](now+1, 0))
	if err := k.MigrationQueue.Walk(ctx, rng, func(key collections.Pair[int64, uint64]) (bool, error) {
		if key.K1() > now {
			return true, nil
		}
		due = append(due, entry{at: key.K1(), id: key.K2()})
		return len(due) >= maxN, nil
	}); err != nil {
		return err
	}
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	for _, e := range due {
		// The queue entry goes first, on the deliver state, so a migration
		// that fails is never retried block after block.
		if err := k.MigrationQueue.Remove(ctx, collections.Join(e.at, e.id)); err != nil {
			return err
		}
		cacheCtx, write := sdkCtx.CacheContext()
		if err := k.FinalizeMigration(cacheCtx, e.id); err != nil {
			sdkCtx.Logger().Error("member migration failed", "migration_id", e.id, "error", err)
			if mErr := k.markMigrationFailed(ctx, e.id, err.Error()); mErr != nil {
				sdkCtx.Logger().Error("failed to mark migration failed", "migration_id", e.id, "error", mErr)
			}
			continue
		}
		write()
	}
	return nil
}

// markMigrationFailed records a finalization failure and frees the member's
// pending slot so they can request again once the cause is fixed.
func (k Keeper) markMigrationFailed(ctx context.Context, id uint64, reason string) error {
	m, err := k.MemberMigrations.Get(ctx, id)
	if err != nil {
		return err
	}
	params, err := k.Params.Get(ctx)
	if err != nil {
		return err
	}
	if err := k.clearPendingIndexes(ctx, m, params); err != nil {
		return err
	}
	m.Status = types.MigrationStatus_MIGRATION_STATUS_FAILED
	m.FailureReason = reason
	if err := k.MemberMigrations.Set(ctx, id, m); err != nil {
		return err
	}
	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent("member_migration_failed",
		sdk.NewAttribute("migration_id", fmt.Sprintf("%d", id)),
		sdk.NewAttribute("from", m.FromAddress),
		sdk.NewAttribute("to", m.ToAddress),
		sdk.NewAttribute("reason", reason),
	))
	return nil
}

// expireUnacceptedMigrations walks MigrationAcceptExpiry[(expires_at, id) <= now].
func (k Keeper) expireUnacceptedMigrations(ctx context.Context, now int64, maxN int) error {
	var due []uint64
	rng := new(collections.Range[collections.Pair[int64, uint64]]).
		EndExclusive(collections.Join[int64, uint64](now+1, 0))
	if err := k.MigrationAcceptExpiry.Walk(ctx, rng, func(key collections.Pair[int64, uint64]) (bool, error) {
		if key.K1() > now {
			return true, nil
		}
		due = append(due, key.K2())
		return len(due) >= maxN, nil
	}); err != nil {
		return err
	}
	for _, id := range due {
		m, err := k.MemberMigrations.Get(ctx, id)
		if err != nil || m.Status != types.MigrationStatus_MIGRATION_STATUS_REQUESTED {
			continue
		}
		if err := k.cancelMigration(ctx, &m, types.MigrationStatus_MIGRATION_STATUS_EXPIRED, "new address did not accept within migration_accept_window"); err != nil {
			return err
		}
	}
	return nil
}

// ActivatePendingZkKeys installs pending ZK public keys whose activation
// epoch has arrived and marks the members dirty for the trust tree rebuild.
func (k Keeper) ActivatePendingZkKeys(ctx context.Context, maxN int) error {
	epoch, err := k.GetCurrentEpoch(ctx)
	if err != nil {
		return err
	}
	type entry struct {
		epoch int64
		addr  string
	}
	var due []entry
	rng := new(collections.Range[collections.Pair[int64, string]]).
		EndExclusive(collections.Join[int64, string](epoch+1, ""))
	if err := k.ZkKeyActivationQueue.Walk(ctx, rng, func(key collections.Pair[int64, string]) (bool, error) {
		if key.K1() > epoch {
			return true, nil
		}
		due = append(due, entry{epoch: key.K1(), addr: key.K2()})
		return len(due) >= maxN, nil
	}); err != nil {
		return err
	}
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	for _, e := range due {
		if err := k.ZkKeyActivationQueue.Remove(ctx, collections.Join(e.epoch, e.addr)); err != nil {
			return err
		}
		member, err := k.Member.Get(ctx, e.addr)
		if err != nil || len(member.PendingZkPublicKey) == 0 || member.ZkKeyActivatesAtEpoch != e.epoch {
			continue // stale entry: the member migrated, was cleared, or re-queued
		}
		member.ZkPublicKey = member.PendingZkPublicKey
		member.PendingZkPublicKey = nil
		member.ZkKeyActivatesAtEpoch = 0
		if err := k.Member.Set(ctx, e.addr, member); err != nil {
			return err
		}
		k.MarkMemberDirty(ctx, e.addr)
		sdkCtx.EventManager().EmitEvent(sdk.NewEvent("rep.zk_public_key_activated",
			sdk.NewAttribute("member", e.addr),
			sdk.NewAttribute("epoch", fmt.Sprintf("%d", e.epoch)),
		))
	}
	return nil
}

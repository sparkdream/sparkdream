package keeper

import (
	"context"

	"cosmossdk.io/collections"

	"sparkdream/x/shield/types"
)

// IsNullifierUsed checks if a nullifier has been used in a given domain and scope.
func (k Keeper) IsNullifierUsed(ctx context.Context, domain uint32, scope uint64, nullifierHex string) bool {
	has, err := k.UsedNullifiers.Has(ctx, collections.Join3(domain, scope, nullifierHex))
	if err != nil {
		return false
	}
	return has
}

// RecordNullifier stores a used nullifier.
func (k Keeper) RecordNullifier(ctx context.Context, domain uint32, scope uint64, nullifierHex string, height int64) error {
	return k.UsedNullifiers.Set(ctx, collections.Join3(domain, scope, nullifierHex), types.UsedNullifier{
		Domain:       domain,
		Scope:        scope,
		NullifierHex: nullifierHex,
		UsedAtHeight: height,
	})
}

// GetUsedNullifier returns a used nullifier record.
func (k Keeper) GetUsedNullifier(ctx context.Context, domain uint32, scope uint64, nullifierHex string) (types.UsedNullifier, bool) {
	n, err := k.UsedNullifiers.Get(ctx, collections.Join3(domain, scope, nullifierHex))
	if err != nil {
		return types.UsedNullifier{}, false
	}
	return n, true
}

// IsPendingNullifier checks if a nullifier is in the pending dedup set (encrypted batch).
func (k Keeper) IsPendingNullifier(ctx context.Context, nullifierHex string) bool {
	has, err := k.PendingNullifiers.Has(ctx, nullifierHex)
	if err != nil {
		return false
	}
	return has
}

// RecordPendingNullifier adds a nullifier to the pending dedup set.
func (k Keeper) RecordPendingNullifier(ctx context.Context, nullifierHex string) error {
	return k.PendingNullifiers.Set(ctx, nullifierHex, true)
}

// DeletePendingNullifier removes a nullifier from the pending dedup set.
func (k Keeper) DeletePendingNullifier(ctx context.Context, nullifierHex string) error {
	return k.PendingNullifiers.Remove(ctx, nullifierHex)
}

// IterateUsedNullifiers iterates over all used nullifiers.
func (k Keeper) IterateUsedNullifiers(ctx context.Context, fn func(types.UsedNullifier) bool) error {
	iter, err := k.UsedNullifiers.Iterate(ctx, nil)
	if err != nil {
		return err
	}
	defer iter.Close()

	for ; iter.Valid(); iter.Next() {
		val, err := iter.Value()
		if err != nil {
			return err
		}
		if fn(val) {
			break
		}
	}
	return nil
}

// IteratePendingNullifiers iterates over all pending nullifiers.
func (k Keeper) IteratePendingNullifiers(ctx context.Context, fn func(nullifierHex string) bool) error {
	iter, err := k.PendingNullifiers.Iterate(ctx, nil)
	if err != nil {
		return err
	}
	defer iter.Close()

	for ; iter.Valid(); iter.Next() {
		key, err := iter.Key()
		if err != nil {
			return err
		}
		if fn(key) {
			break
		}
	}
	return nil
}

// PruneEpochScopedNullifiers removes epoch-scoped nullifiers older than cutoffEpoch.
// It cross-references registered operations to find the domains that use EPOCH
// scoping, then prunes only those whose window ends before cutoffEpoch (scope
// < cutoffEpoch / epoch_window; the largest window if a domain's ops differ).
// GLOBAL-scoped (scope=0) and MESSAGE_FIELD-scoped nullifiers are not pruned.
//
// Nullifiers are keyed (domain, scope, hex), so each epoch domain is pruned by
// a range scan over exactly the stale keys; the ever-growing MESSAGE_FIELD
// nullifiers (one per member per voted target) are never visited.
func (k Keeper) PruneEpochScopedNullifiers(ctx context.Context, cutoffEpoch uint64) error {
	// Domains that use EPOCH scoping, in registration order, with the
	// largest window among their ops so no live window is pruned.
	var epochDomains []uint32
	windows := make(map[uint32]uint64)
	if err := k.IterateShieldedOps(ctx, func(_ string, reg types.ShieldedOpRegistration) bool {
		if reg.NullifierScopeType != types.NullifierScopeType_NULLIFIER_SCOPE_EPOCH {
			return false
		}
		if _, seen := windows[reg.NullifierDomain]; !seen {
			epochDomains = append(epochDomains, reg.NullifierDomain)
		}
		windows[reg.NullifierDomain] = max(windows[reg.NullifierDomain], reg.Window())
		return false
	}); err != nil {
		return err
	}

	for _, domain := range epochDomains {
		// [(domain, 0, ""), (domain, cutoffScope, "")) covers every scope
		// below the cutoff and nothing else.
		cutoffScope := cutoffEpoch / windows[domain]
		rng := new(collections.Range[collections.Triple[uint32, uint64, string]]).
			StartInclusive(collections.Join3(domain, uint64(0), "")).
			EndExclusive(collections.Join3(domain, cutoffScope, ""))
		iter, err := k.UsedNullifiers.Iterate(ctx, rng)
		if err != nil {
			return err
		}
		// Collect first: the store can't be modified while iterating.
		keys, err := iter.Keys()
		if err != nil {
			return err
		}
		for _, key := range keys {
			if err := k.UsedNullifiers.Remove(ctx, key); err != nil {
				return err
			}
		}
	}

	return nil
}

package keeper

import "context"

// Nil-safe wrappers around x/rep's alias contract (member key migration,
// docs/x-rep-spec.md). The rep keeper is late-wired after depinject and is
// nil in unit tests, where every helper degrades to the plain string
// comparison it replaces.

// canonical returns the member's current address (addr itself when it never
// migrated).
func (k Keeper) canonical(ctx context.Context, addr string) string {
	return k.recentAddressesOf(ctx, addr)[0]
}

// canActAs reports whether signer may exercise rights recorded against the
// stored address (not tombstoned, and stored resolves to signer).
func (k Keeper) canActAs(ctx context.Context, signer, stored string) bool {
	if k.late.repKeeper == nil {
		return signer == stored
	}
	return k.late.repKeeper.CanActAs(ctx, signer, stored)
}

// sameMember reports whether two stored addresses belong to one member.
func (k Keeper) sameMember(ctx context.Context, a, b string) bool {
	if k.late.repKeeper == nil {
		return a == b
	}
	return k.late.repKeeper.SameMember(ctx, a, b)
}

// recentAddressesOf returns the member's canonical address plus the
// predecessors inside the dedup horizon (bounded, gas-cheap).
func (k Keeper) recentAddressesOf(ctx context.Context, member string) []string {
	if k.late.repKeeper == nil {
		return []string{member}
	}
	return k.late.repKeeper.RecentAddressesOf(ctx, member)
}

// allAddressesOf returns the member's canonical address plus every
// predecessor. Queries only; never call it inside a message handler.
func (k Keeper) allAddressesOf(ctx context.Context, member string) []string {
	if k.late.repKeeper == nil {
		return []string{member}
	}
	return k.late.repKeeper.AllAddressesOf(ctx, member)
}

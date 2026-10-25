package keeper

import "context"

// Nil-safe wrappers around x/rep's alias contract (member key migration,
// docs/x-rep-spec.md). The rep keeper is late-wired after depinject and is
// nil in unit tests, where every helper degrades to the plain string
// comparison it replaces.

// canActAs reports whether signer may exercise rights recorded against the
// stored address (not tombstoned, and stored resolves to signer).
func (k Keeper) canActAs(ctx context.Context, signer, stored string) bool {
	if k.repKeeper == nil {
		return signer == stored
	}
	return k.repKeeper.CanActAs(ctx, signer, stored)
}

// sameMember reports whether two stored addresses belong to one member.
func (k Keeper) sameMember(ctx context.Context, a, b string) bool {
	if k.repKeeper == nil {
		return a == b
	}
	return k.repKeeper.SameMember(ctx, a, b)
}

// recentAddressesOf returns the member's canonical address plus the
// predecessors inside the dedup horizon (bounded, gas-cheap).
func (k Keeper) recentAddressesOf(ctx context.Context, member string) []string {
	if k.repKeeper == nil {
		return []string{member}
	}
	return k.repKeeper.RecentAddressesOf(ctx, member)
}

// allAddressesOf returns the member's canonical address plus every
// predecessor. Queries only; never call it inside a message handler.
func (k Keeper) allAddressesOf(ctx context.Context, member string) []string {
	if k.repKeeper == nil {
		return []string{member}
	}
	return k.repKeeper.AllAddressesOf(ctx, member)
}

package keeper

import (
	"context"
	"math"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/rep/types"
)

// The alias contract (docs/x-rep-spec.md, "Member key migration", §6 of the
// plan). A finalized migration leaves a Tombstone at the old address that
// forwards to the canonical one. Everything that keeps storing the old
// address (authorship, stakes, votes, escrows) is interpreted through these
// helpers:
//
//   - value lookups resolve the alias (GetMember, GetTrustLevel, every DREAM
//     primitive): a stake record that says A still finds the member;
//   - gates refuse tombstones (IsActiveMember, CanActAs): a thief holding A's
//     key must not pass a membership gate because A forwards to B;
//   - dedup checks union over RecentAddressesOf (bounded, gas-cheap);
//   - self-dealing bans compare Canonical addresses (two O(1) reads);
//   - list queries union over AllAddressesOf (paginated, no consensus gas).

// Canonical returns the current address for addr: addr itself when it never
// migrated, otherwise the address its tombstone points at. Tombstones are
// path-compressed at finalization, so this is always one store read.
func (k Keeper) Canonical(ctx context.Context, addr string) string {
	t, err := k.Tombstones.Get(ctx, addr)
	if err != nil {
		return addr
	}
	return t.Canonical
}

// CanonicalAccAddress is Canonical for a binary address.
func (k Keeper) CanonicalAccAddress(ctx context.Context, addr sdk.AccAddress) sdk.AccAddress {
	str, err := k.addressCodec.BytesToString(addr)
	if err != nil {
		return addr
	}
	canon := k.Canonical(ctx, str)
	if canon == str {
		return addr
	}
	bz, err := k.addressCodec.StringToBytes(canon)
	if err != nil {
		return addr
	}
	return bz
}

// resolveMember maps a stored address to the member record that now carries
// it. Used at the top of every value lookup and DREAM primitive so a record
// written against a tombstone unlocks, burns or mints against the canonical
// member. Gates must NOT call it.
func (k Keeper) resolveMember(ctx context.Context, addr sdk.AccAddress) sdk.AccAddress {
	return k.CanonicalAccAddress(ctx, addr)
}

// IsTombstoned reports whether addr has migrated away.
func (k Keeper) IsTombstoned(ctx context.Context, addr string) bool {
	has, err := k.Tombstones.Has(ctx, addr)
	return err == nil && has
}

// GetTombstone returns the tombstone at addr, if any.
func (k Keeper) GetTombstone(ctx context.Context, addr string) (types.Tombstone, bool) {
	t, err := k.Tombstones.Get(ctx, addr)
	if err != nil {
		return types.Tombstone{}, false
	}
	return t, true
}

// CanActAs reports whether signer may exercise rights recorded against the
// stored address: true iff signer is not tombstoned and Canonical(stored) ==
// signer. It replaces every `stored != signer` authorization comparison. The
// non-tombstoned clause is what stops a thief who still holds the old key
// from editing posts, cancelling bounties or moving tokens after the real
// owner migrated away.
func (k Keeper) CanActAs(ctx context.Context, signer, stored string) bool {
	if signer == "" || stored == "" {
		return false
	}
	if k.IsTombstoned(ctx, signer) {
		return false
	}
	return k.Canonical(ctx, stored) == signer
}

// SameMember reports whether two stored addresses belong to the same member
// (self-dealing bans: a juror on their own case, a contributor staking on
// their own reveal, an author upvoting their own post). Two O(1) lookups,
// complete over the whole history.
func (k Keeper) SameMember(ctx context.Context, a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	return k.Canonical(ctx, a) == k.Canonical(ctx, b)
}

// RecentAddressesOf returns the canonical address of member followed by its
// predecessors that stopped being canonical within AliasDedupHorizonEpochs,
// at most MaxRecentAliases of them. It is the only form allowed inside other
// modules' gas-tight transactions: one tombstone read plus a bounded range
// scan of the reverse index.
func (k Keeper) RecentAddressesOf(ctx context.Context, member string) []string {
	canonical := k.Canonical(ctx, member)
	out := []string{canonical}
	epoch, err := k.GetCurrentEpoch(ctx)
	if err != nil {
		return out
	}
	cutoff := epoch - types.AliasDedupHorizonEpochs
	if cutoff < 0 {
		cutoff = 0
	}
	rng := new(collections.Range[collections.Triple[string, int64, string]]).
		StartInclusive(collections.Join3(canonical, cutoff, "")).
		EndInclusive(collections.Join3(canonical, int64(math.MaxInt64), ""))
	if err := k.MigratedFrom.Walk(ctx, rng, func(key collections.Triple[string, int64, string]) (bool, error) {
		if key.K1() != canonical {
			return true, nil
		}
		out = append(out, key.K3())
		return len(out) > types.MaxRecentAliases, nil
	}); err != nil {
		sdk.UnwrapSDKContext(ctx).Logger().Error("RecentAddressesOf: MigratedFrom.Walk failed", "err", err)
	}
	return out
}

// AllAddressesOf returns the canonical address of member followed by every
// predecessor (at most MaxLifetimeAliases). For checks with gas slack that
// need the full history (gift limits, warnings, escalation) and for paged
// list queries.
func (k Keeper) AllAddressesOf(ctx context.Context, member string) []string {
	canonical := k.Canonical(ctx, member)
	out := []string{canonical}
	rng := collections.NewPrefixedTripleRange[string, int64, string](canonical)
	if err := k.MigratedFrom.Walk(ctx, rng, func(key collections.Triple[string, int64, string]) (bool, error) {
		out = append(out, key.K3())
		return len(out) > types.MaxLifetimeAliases, nil
	}); err != nil {
		sdk.UnwrapSDKContext(ctx).Logger().Error("AllAddressesOf: MigratedFrom.Walk failed", "err", err)
	}
	return out
}

// aliasCounts returns how many aliases canonical has in total and how many
// are inside the dedup horizon. Both are read against the request-time caps.
func (k Keeper) aliasCounts(ctx context.Context, canonical string) (total, recent int) {
	epoch, getErr := k.GetCurrentEpoch(ctx)
	if getErr != nil {
		sdk.UnwrapSDKContext(ctx).Logger().Error("GetCurrentEpoch failed", "err", getErr)
	}
	cutoff := epoch - types.AliasDedupHorizonEpochs
	rng := collections.NewPrefixedTripleRange[string, int64, string](canonical)
	if err := k.MigratedFrom.Walk(ctx, rng, func(key collections.Triple[string, int64, string]) (bool, error) {
		total++
		if key.K2() >= cutoff {
			recent++
		}
		return false, nil
	}); err != nil {
		sdk.UnwrapSDKContext(ctx).Logger().Error("aliasCounts: MigratedFrom.Walk failed", "err", err)
	}
	return total, recent
}

// AddressesMatch reports whether any address in addrs equals target. A small
// helper for dedup call sites that union over RecentAddressesOf.
func AddressesMatch(addrs []string, target string) bool {
	for _, a := range addrs {
		if a == target {
			return true
		}
	}
	return false
}

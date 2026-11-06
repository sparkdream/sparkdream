package keeper

import (
	"fmt"
	"strings"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/session/types"
)

// RegisterInvariants registers the x/session invariants (run by app's
// TestInvariants; x/crisis is not wired).
func RegisterInvariants(ir sdk.InvariantRegistry, k Keeper) {
	ir.RegisterRoute(types.ModuleName, "grant-indexes", GrantIndexesInvariant(k))
	ir.RegisterRoute(types.ModuleName, "active-grant-counts", ActiveGrantCountsInvariant(k))
	ir.RegisterRoute(types.ModuleName, "oneshot-deposits", OneshotDepositsInvariant(k))
	ir.RegisterRoute(types.ModuleName, "pending-grants", PendingGrantsInvariant(k))
}

func invariantResult(name string, problems []string) (string, bool) {
	broken := len(problems) > 0
	return sdk.FormatInvariant(types.ModuleName, name,
		fmt.Sprintf("%d problem(s)\n%s", len(problems), strings.Join(problems, "\n"))), broken
}

// GrantIndexesInvariant: every grant is in the granter and type indexes and
// a SESSION_KEY grant owns its pair lookup; an active grant is also in the
// grantee, grantee-and-type and expiration indexes, while a PENDING one is
// in none of those (SES-102) and sits on the pending-deadline index instead;
// an ACTIVE oneshot sits on the fire-time index and a paused one on the
// paused-TTL index.
func GrantIndexesInvariant(k Keeper) sdk.Invariant {
	return func(ctx sdk.Context) (string, bool) {
		var problems []string
		if err := k.Grants.Walk(ctx, nil, func(id uint64, g types.Grant) (bool, error) {
			check := func(ok bool, err error, what string) {
				if err != nil || !ok {
					problems = append(problems, fmt.Sprintf("grant %d missing from %s", id, what))
				}
			}
			absent := func(ok bool, err error, what string) {
				if err != nil || ok {
					problems = append(problems, fmt.Sprintf("pending grant %d present in %s", id, what))
				}
			}
			ok, err := k.GrantsByGranter.Has(ctx, collections.Join(g.Granter, id))
			check(ok, err, "GrantsByGranter")
			ok, err = k.GrantsByTypeAndGranter.Has(ctx, collections.Join3(int32(g.Type), g.Granter, id))
			check(ok, err, "GrantsByTypeAndGranter")
			if g.Type == types.GrantType_GRANT_TYPE_SESSION_KEY {
				pid, err := k.SessionKeyByPair.Get(ctx, collections.Join(g.Granter, g.Grantee))
				check(pid == id, err, "SessionKeyByPair")
			}
			if g.Status == types.GrantStatus_GRANT_STATUS_PENDING {
				if !types.IsAccessGrantType(g.Type) {
					problems = append(problems, fmt.Sprintf("grant %d is a pending %s", id, g.Type))
				}
				ok, err = k.PendingGrantsByDeadline.Has(ctx, collections.Join(pendingDeadline(g), id))
				check(ok, err, "PendingGrantsByDeadline")
				ok, err = k.PendingGrantsByGrantee.Has(ctx, collections.Join(g.Grantee, id))
				check(ok, err, "PendingGrantsByGrantee")
				ok, err = k.GrantsByGrantee.Has(ctx, collections.Join(g.Grantee, id))
				absent(ok, err, "GrantsByGrantee")
				ok, err = k.GrantsByGranteeAndType.Has(ctx, collections.Join3(g.Grantee, int32(g.Type), id))
				absent(ok, err, "GrantsByGranteeAndType")
				ok, err = k.GrantsByExpiration.Has(ctx, collections.Join(g.ExpiresAt.Unix(), id))
				absent(ok, err, "GrantsByExpiration")
				return false, nil
			}
			ok, err = k.GrantsByGrantee.Has(ctx, collections.Join(g.Grantee, id))
			check(ok, err, "GrantsByGrantee")
			ok, err = k.GrantsByGranteeAndType.Has(ctx, collections.Join3(g.Grantee, int32(g.Type), id))
			check(ok, err, "GrantsByGranteeAndType")
			ok, err = k.GrantsByExpiration.Has(ctx, collections.Join(g.ExpiresAt.Unix(), id))
			check(ok, err, "GrantsByExpiration")
			ok, err = k.PendingGrantsByGrantee.Has(ctx, collections.Join(g.Grantee, id))
			absent(ok, err, "PendingGrantsByGrantee")
			if so := g.GetScheduledOneshot(); so != nil {
				switch g.Status {
				case types.GrantStatus_GRANT_STATUS_ACTIVE:
					ok, err = k.GrantsByFireTime.Has(ctx, collections.Join(so.FireAt, id))
					check(ok, err, "GrantsByFireTime")
				case types.GrantStatus_GRANT_STATUS_PAUSED_INSUFFICIENT_FUNDS:
					ok, err = k.PausedOneshotByPauseTime.Has(ctx, collections.Join(so.FireAt, id))
					check(ok, err, "PausedOneshotByPauseTime")
				}
			}
			return false, nil
		}); err != nil {
			return fmt.Sprintf("Grants.Walk failed: %v", err), true
		}
		return invariantResult("grant-indexes", problems)
	}
}

// ActiveGrantCountsInvariant: ActiveGrantCountByType equals the number of
// ACTIVE or PAUSED grants per (granter, type).
func ActiveGrantCountsInvariant(k Keeper) sdk.Invariant {
	return func(ctx sdk.Context) (string, bool) {
		var problems []string
		// A collections.Pair holds pointers, so it is not a usable map key:
		// key by value.
		type countKey struct {
			granter string
			typ     int32
		}
		want := map[countKey]uint32{}
		if err := k.Grants.Walk(ctx, nil, func(_ uint64, g types.Grant) (bool, error) {
			if activeStatus(g.Status) {
				want[countKey{g.Granter, int32(g.Type)}]++
			}
			return false, nil
		}); err != nil {
			return fmt.Sprintf("Grants.Walk failed: %v", err), true
		}
		if err := k.ActiveGrantCountByType.Walk(ctx, nil, func(key collections.Pair[string, int32], n uint32) (bool, error) {
			ck := countKey{key.K1(), key.K2()}
			if want[ck] != n {
				problems = append(problems, fmt.Sprintf("(%s, %d): counter %d, actual %d", key.K1(), key.K2(), n, want[ck]))
			}
			delete(want, ck)
			return false, nil
		}); err != nil {
			return fmt.Sprintf("ActiveGrantCountByType.Walk failed: %v", err), true
		}
		for key, n := range want {
			if n > 0 {
				problems = append(problems, fmt.Sprintf("(%s, %d): no counter, actual %d", key.granter, key.typ, n))
			}
		}
		return invariantResult("active-grant-counts", problems)
	}
}

// OneshotDepositsInvariant: every escrowed deposit belongs to an existing
// SCHEDULED_ONESHOT grant, and the module account holds at least the sum.
func OneshotDepositsInvariant(k Keeper) sdk.Invariant {
	return func(ctx sdk.Context) (string, bool) {
		var problems []string
		total := sdk.NewCoins()
		if err := k.OneshotGasDeposit.Walk(ctx, nil, func(id uint64, c sdk.Coin) (bool, error) {
			g, err := k.Grants.Get(ctx, id)
			if err != nil || g.Type != types.GrantType_GRANT_TYPE_SCHEDULED_ONESHOT {
				problems = append(problems, fmt.Sprintf("deposit %s for grant %d has no oneshot grant", c, id))
			}
			total = total.Add(c)
			return false, nil
		}); err != nil {
			return fmt.Sprintf("OneshotGasDeposit.Walk failed: %v", err), true
		}
		if !total.IsZero() {
			held := k.bankKeeper.SpendableCoins(ctx, k.authKeeper.GetModuleAddress(types.ModuleName))
			if !held.IsAllGTE(total) {
				problems = append(problems, fmt.Sprintf("module holds %s, deposits total %s", held, total))
			}
		}
		return invariantResult("oneshot-deposits", problems)
	}
}

// PendingGrantsInvariant (SES-102): PendingGrantCountByGranter equals the
// number of PENDING grants per granter; every pending deposit belongs to an
// existing PENDING grant; every PendingGrantsByGrantee entry names a PENDING
// grant of that grantee (SES-202; the forward check is in grant-indexes);
// the module account holds at least the pending and oneshot deposits
// together; every allowed-granter entry has its reverse entry and vice
// versa.
func PendingGrantsInvariant(k Keeper) sdk.Invariant {
	return func(ctx sdk.Context) (string, bool) {
		var problems []string
		want := map[string]uint32{}
		if err := k.Grants.Walk(ctx, nil, func(_ uint64, g types.Grant) (bool, error) {
			if g.Status == types.GrantStatus_GRANT_STATUS_PENDING {
				want[g.Granter]++
			}
			return false, nil
		}); err != nil {
			return fmt.Sprintf("Grants.Walk failed: %v", err), true
		}
		if err := k.PendingGrantCountByGranter.Walk(ctx, nil, func(granter string, n uint32) (bool, error) {
			if want[granter] != n {
				problems = append(problems, fmt.Sprintf("%s: pending counter %d, actual %d", granter, n, want[granter]))
			}
			if n > types.MaxPendingGrantsPerGranter {
				problems = append(problems, fmt.Sprintf("%s: %d pending grants exceeds %d", granter, n, types.MaxPendingGrantsPerGranter))
			}
			delete(want, granter)
			return false, nil
		}); err != nil {
			return fmt.Sprintf("PendingGrantCountByGranter.Walk failed: %v", err), true
		}
		for granter, n := range want {
			if n > 0 {
				problems = append(problems, fmt.Sprintf("%s: no pending counter, actual %d", granter, n))
			}
		}

		if err := k.PendingGrantsByGrantee.Walk(ctx, nil, func(key collections.Pair[string, uint64]) (bool, error) {
			g, err := k.Grants.Get(ctx, key.K2())
			if err != nil || g.Status != types.GrantStatus_GRANT_STATUS_PENDING || g.Grantee != key.K1() {
				problems = append(problems, fmt.Sprintf("PendingGrantsByGrantee entry (%s, %d) has no pending grant", key.K1(), key.K2()))
			}
			return false, nil
		}); err != nil {
			return fmt.Sprintf("PendingGrantsByGrantee.Walk failed: %v", err), true
		}

		total := sdk.NewCoins()
		if err := k.PendingGrantDeposit.Walk(ctx, nil, func(id uint64, c sdk.Coin) (bool, error) {
			g, err := k.Grants.Get(ctx, id)
			if err != nil || g.Status != types.GrantStatus_GRANT_STATUS_PENDING {
				problems = append(problems, fmt.Sprintf("pending deposit %s for grant %d has no pending grant", c, id))
			}
			total = total.Add(c)
			return false, nil
		}); err != nil {
			return fmt.Sprintf("PendingGrantDeposit.Walk failed: %v", err), true
		}
		if err := k.OneshotGasDeposit.Walk(ctx, nil, func(_ uint64, c sdk.Coin) (bool, error) {
			total = total.Add(c)
			return false, nil
		}); err != nil {
			return fmt.Sprintf("OneshotGasDeposit.Walk failed: %v", err), true
		}
		if !total.IsZero() {
			held := k.bankKeeper.SpendableCoins(ctx, k.authKeeper.GetModuleAddress(types.ModuleName))
			if !held.IsAllGTE(total) {
				problems = append(problems, fmt.Sprintf("module holds %s, deposits total %s", held, total))
			}
		}

		if err := k.AllowedGranters.Walk(ctx, nil, func(key collections.Pair[string, string]) (bool, error) {
			ok, err := k.AllowedGranteesByGranter.Has(ctx, collections.Join(key.K2(), key.K1()))
			if err != nil || !ok {
				problems = append(problems, fmt.Sprintf("allowed granter (%s, %s) has no reverse entry", key.K1(), key.K2()))
			}
			return false, nil
		}); err != nil {
			return fmt.Sprintf("AllowedGranters.Walk failed: %v", err), true
		}
		if err := k.AllowedGranteesByGranter.Walk(ctx, nil, func(key collections.Pair[string, string]) (bool, error) {
			ok, err := k.AllowedGranters.Has(ctx, collections.Join(key.K2(), key.K1()))
			if err != nil || !ok {
				problems = append(problems, fmt.Sprintf("reverse entry (%s, %s) has no allowed granter", key.K1(), key.K2()))
			}
			return false, nil
		}); err != nil {
			return fmt.Sprintf("AllowedGranteesByGranter.Walk failed: %v", err), true
		}
		return invariantResult("pending-grants", problems)
	}
}

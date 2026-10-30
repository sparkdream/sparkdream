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
}

func invariantResult(name string, problems []string) (string, bool) {
	broken := len(problems) > 0
	return sdk.FormatInvariant(types.ModuleName, name,
		fmt.Sprintf("%d problem(s)\n%s", len(problems), strings.Join(problems, "\n"))), broken
}

// GrantIndexesInvariant: every grant is in the granter, grantee,
// expiration and type indexes; a SESSION_KEY grant owns its pair lookup;
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
			ok, err := k.GrantsByGranter.Has(ctx, collections.Join(g.Granter, id))
			check(ok, err, "GrantsByGranter")
			ok, err = k.GrantsByGrantee.Has(ctx, collections.Join(g.Grantee, id))
			check(ok, err, "GrantsByGrantee")
			ok, err = k.GrantsByExpiration.Has(ctx, collections.Join(g.ExpiresAt.Unix(), id))
			check(ok, err, "GrantsByExpiration")
			ok, err = k.GrantsByTypeAndGranter.Has(ctx, collections.Join3(int32(g.Type), g.Granter, id))
			check(ok, err, "GrantsByTypeAndGranter")
			if g.Type == types.GrantType_GRANT_TYPE_SESSION_KEY {
				pid, err := k.SessionKeyByPair.Get(ctx, collections.Join(g.Granter, g.Grantee))
				check(pid == id, err, "SessionKeyByPair")
			}
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
		want := map[collections.Pair[string, int32]]uint32{}
		if err := k.Grants.Walk(ctx, nil, func(_ uint64, g types.Grant) (bool, error) {
			if activeStatus(g.Status) {
				want[collections.Join(g.Granter, int32(g.Type))]++
			}
			return false, nil
		}); err != nil {
			return fmt.Sprintf("Grants.Walk failed: %v", err), true
		}
		if err := k.ActiveGrantCountByType.Walk(ctx, nil, func(key collections.Pair[string, int32], n uint32) (bool, error) {
			if want[key] != n {
				problems = append(problems, fmt.Sprintf("(%s, %d): counter %d, actual %d", key.K1(), key.K2(), n, want[key]))
			}
			delete(want, key)
			return false, nil
		}); err != nil {
			return fmt.Sprintf("ActiveGrantCountByType.Walk failed: %v", err), true
		}
		for key, n := range want {
			if n > 0 {
				problems = append(problems, fmt.Sprintf("(%s, %d): no counter, actual %d", key.K1(), key.K2(), n))
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

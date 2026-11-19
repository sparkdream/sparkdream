package keeper

import (
	"fmt"

	"sparkdream/x/commons/types"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// RegisterInvariants registers the commons invariants. x/crisis is not wired
// on chain; app's TestInvariants runs every module's RegisterInvariants.
func RegisterInvariants(ir sdk.InvariantRegistry, k Keeper) {
	ir.RegisterRoute(types.ModuleName, "policy-index", PolicyIndexInvariant(k))
	ir.RegisterRoute(types.ModuleName, "member-bounds", MemberBoundsInvariant(k))
	ir.RegisterRoute(types.ModuleName, "golden-share", GoldenShareInvariant(k))
	ir.RegisterRoute(types.ModuleName, "treasury-stakers", TreasuryStakersInvariant(k))
}

// TreasuryStakersInvariant: every group whose standard policy address holds
// a delegation is in the TreasuryStakers index, and every index entry is the
// standard policy of a live group. The treasury stake cap and the x/gov
// tally read the index, so a staked treasury missing from it would vote and
// escape the cap.
func TreasuryStakersInvariant(k Keeper) sdk.Invariant {
	return func(ctx sdk.Context) (string, bool) {
		var problems string
		sk := k.late.stakingKeeper
		if err := k.Groups.Walk(ctx, nil, func(name string, g types.Group) (bool, error) {
			if sk == nil || g.PolicyAddress == "" {
				return false, nil
			}
			bz, err := k.addressCodec.StringToBytes(g.PolicyAddress)
			if err != nil {
				return false, nil
			}
			delegations, err := sk.GetDelegatorDelegations(ctx, bz, 1)
			if err != nil {
				return true, err
			}
			if len(delegations) == 0 {
				return false, nil
			}
			indexed, err := k.TreasuryStakers.Has(ctx, g.PolicyAddress)
			if err != nil {
				return true, err
			}
			if !indexed {
				problems += fmt.Sprintf("group %s (%s) holds a delegation but is not in TreasuryStakers\n", name, g.PolicyAddress)
			}
			return false, nil
		}); err != nil {
			problems += fmt.Sprintf("walking groups failed: %v\n", err)
		}
		if err := k.TreasuryStakers.Walk(ctx, nil, func(policy string) (bool, error) {
			if _, group, found := k.GetGroupByPolicy(ctx, policy); !found || group.PolicyAddress != policy {
				problems += fmt.Sprintf("TreasuryStakers entry %s is not the standard policy of a live group\n", policy)
			}
			return false, nil
		}); err != nil {
			problems += fmt.Sprintf("walking TreasuryStakers failed: %v\n", err)
		}
		return sdk.FormatInvariant(types.ModuleName, "treasury-stakers", problems), problems != ""
	}
}

// PolicyIndexInvariant: every group's policy (and veto policy) maps back to
// the group in PolicyToName, and every PolicyToName entry names a live group.
func PolicyIndexInvariant(k Keeper) sdk.Invariant {
	return func(ctx sdk.Context) (string, bool) {
		var problems string
		if err := k.Groups.Walk(ctx, nil, func(name string, g types.Group) (bool, error) {
			for _, addr := range []string{g.PolicyAddress, g.VetoPolicyAddress} {
				if addr == "" {
					continue
				}
				if got, err := k.PolicyToName.Get(ctx, addr); err != nil || got != name {
					problems += fmt.Sprintf("group %q: policy %s indexes to %q (err %v)\n", name, addr, got, err)
				}
			}
			return false, nil
		}); err != nil {
			return fmt.Sprintf("Groups.Walk failed: %v", err), true
		}
		if err := k.PolicyToName.Walk(ctx, nil, func(addr, name string) (bool, error) {
			if has, _ := k.Groups.Has(ctx, name); !has {
				problems += fmt.Sprintf("policy %s indexes to missing group %q\n", addr, name)
			}
			return false, nil
		}); err != nil {
			return fmt.Sprintf("PolicyToName.Walk failed: %v", err), true
		}
		return sdk.FormatInvariant(types.ModuleName, "policy-index", problems), problems != ""
	}
}

// OperationalMembers is the seat count from which a group is operational:
// a standing committee is seeded with the founder alone, below its
// min_members by design, until its first add; from two seats on it can
// never shrink below min_members again (docs/security-hardening.md, one-way
// ratchet).
const OperationalMembers uint64 = 2

// MemberBoundsInvariant: every group has at least one member and at most
// max_members, and an operational group (OperationalMembers seats or more)
// holds at least min_members (CMN-103).
func MemberBoundsInvariant(k Keeper) sdk.Invariant {
	return func(ctx sdk.Context) (string, bool) {
		var problems string
		if err := k.Groups.Walk(ctx, nil, func(name string, g types.Group) (bool, error) {
			n, err := k.CountCouncilMembers(ctx, name)
			if err != nil {
				problems += fmt.Sprintf("group %q: %v\n", name, err)
				return false, nil
			}
			if g.MaxMembers > 0 && n > g.MaxMembers {
				problems += fmt.Sprintf("group %q: %d members > max %d\n", name, n, g.MaxMembers)
			}
			if n == 0 {
				problems += fmt.Sprintf("group %q has no members\n", name)
			}
			if n >= OperationalMembers && n < g.MinMembers {
				problems += fmt.Sprintf("group %q: %d members < min %d\n", name, n, g.MinMembers)
			}
			return false, nil
		}); err != nil {
			return fmt.Sprintf("Groups.Walk failed: %v", err), true
		}
		return sdk.FormatInvariant(types.ModuleName, "member-bounds", problems), problems != ""
	}
}

// GoldenShareInvariant: in each golden-share council the Commons Council
// seat (when present) weighs exactly the sum of every other member.
func GoldenShareInvariant(k Keeper) sdk.Invariant {
	return func(ctx sdk.Context) (string, bool) {
		var problems string
		for council := range goldenShareCouncils {
			seat, err := k.goldenShareSeat(ctx, council)
			if err != nil || seat == "" {
				continue
			}
			seatWeight := math.LegacyZeroDec()
			others := math.LegacyZeroDec()
			found := false
			rng := collections.NewPrefixedPairRange[string, string](council)
			if err := k.Members.Walk(ctx, rng, func(_ collections.Pair[string, string], m types.Member) (bool, error) {
				w, err := math.LegacyNewDecFromStr(m.Weight)
				if err != nil {
					return false, nil
				}
				if m.Address == seat {
					seatWeight = w
					found = true
				} else {
					others = others.Add(w)
				}
				return false, nil
			}); err != nil {
				return fmt.Sprintf("Members.Walk failed: %v", err), true
			}
			if found && !seatWeight.Equal(others) {
				problems += fmt.Sprintf("%s: golden-share seat weight %s != others %s\n", council, seatWeight, others)
			}
		}
		return sdk.FormatInvariant(types.ModuleName, "golden-share", problems), problems != ""
	}
}

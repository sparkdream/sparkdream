package keeper

import (
	"fmt"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/name/types"
)

// RegisterInvariants registers the x/name invariants. x/crisis is not wired
// on chain; app's TestInvariants runs every module's RegisterInvariants.
func RegisterInvariants(ir sdk.InvariantRegistry, k Keeper) {
	ir.RegisterRoute(types.ModuleName, "owner-index", OwnerIndexInvariant(k))
	ir.RegisterRoute(types.ModuleName, "accepted-targets", AcceptedTargetsInvariant(k))
	ir.RegisterRoute(types.ModuleName, "dispute-stakes", DisputeStakesInvariant(k))
}

// OwnerIndexInvariant: OwnerNames holds exactly (record.owner, record.name)
// for every name record.
func OwnerIndexInvariant(k Keeper) sdk.Invariant {
	return func(ctx sdk.Context) (string, bool) {
		var problems string
		if err := k.Names.Walk(ctx, nil, func(name string, r types.NameRecord) (bool, error) {
			if has, _ := k.OwnerNames.Has(ctx, collections.Join(r.Owner, name)); !has {
				problems += fmt.Sprintf("name %q: owner index missing for %s\n", name, r.Owner)
			}
			return false, nil
		}); err != nil {
			return fmt.Sprintf("Names.Walk failed: %v", err), true
		}
		if err := k.OwnerNames.Walk(ctx, nil, func(key collections.Pair[string, string]) (bool, error) {
			r, err := k.Names.Get(ctx, key.K2())
			if err != nil || r.Owner != key.K1() {
				problems += fmt.Sprintf("owner index (%s, %q) has no matching record\n", key.K1(), key.K2())
			}
			return false, nil
		}); err != nil {
			return fmt.Sprintf("OwnerNames.Walk failed: %v", err), true
		}
		return sdk.FormatInvariant(types.ModuleName, "owner-index", problems), problems != ""
	}
}

// AcceptedTargetsInvariant: AcceptedTargets holds exactly the names whose
// record has target_accepted with that target.
func AcceptedTargetsInvariant(k Keeper) sdk.Invariant {
	return func(ctx sdk.Context) (string, bool) {
		var problems string
		if err := k.AcceptedTargets.Walk(ctx, nil, func(key collections.Pair[string, string]) (bool, error) {
			r, err := k.Names.Get(ctx, key.K2())
			if err != nil || !r.TargetAccepted || r.Target != key.K1() {
				problems += fmt.Sprintf("accepted target (%s, %q) does not match its record\n", key.K1(), key.K2())
			}
			return false, nil
		}); err != nil {
			return fmt.Sprintf("AcceptedTargets.Walk failed: %v", err), true
		}
		if err := k.Names.Walk(ctx, nil, func(name string, r types.NameRecord) (bool, error) {
			if r.TargetAccepted && r.Target != "" {
				if has, _ := k.AcceptedTargets.Has(ctx, collections.Join(r.Target, name)); !has {
					problems += fmt.Sprintf("name %q: accepted target %s not indexed\n", name, r.Target)
				}
			}
			return false, nil
		}); err != nil {
			return fmt.Sprintf("Names.Walk failed: %v", err), true
		}
		return sdk.FormatInvariant(types.ModuleName, "accepted-targets", problems), problems != ""
	}
}

// DisputeStakesInvariant: every active dispute has its claimant stake row,
// and every active contested dispute its contest stake row.
func DisputeStakesInvariant(k Keeper) sdk.Invariant {
	return func(ctx sdk.Context) (string, bool) {
		var problems string
		if err := k.Disputes.Walk(ctx, nil, func(name string, d types.Dispute) (bool, error) {
			if !d.Active {
				return false, nil
			}
			id := fmt.Sprintf("name_dispute:%s:%d", d.Name, d.FiledAt)
			if has, _ := k.DisputeStakes.Has(ctx, id); !has {
				problems += fmt.Sprintf("active dispute %q has no stake %s\n", name, id)
			}
			if d.ContestChallengeId != "" {
				if has, _ := k.ContestStakes.Has(ctx, d.ContestChallengeId); !has {
					problems += fmt.Sprintf("active dispute %q has no contest stake %s\n", name, d.ContestChallengeId)
				}
			}
			return false, nil
		}); err != nil {
			return fmt.Sprintf("Disputes.Walk failed: %v", err), true
		}
		return sdk.FormatInvariant(types.ModuleName, "dispute-stakes", problems), problems != ""
	}
}

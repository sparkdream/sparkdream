package keeper

import (
	"context"
	"fmt"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/name/types"
	reptypes "sparkdream/x/rep/types"
)

// MigrationHooks implements x/rep's MemberMigrationHooks for x/name
// (member-key-migration plan §8, name row). Names are identity, so they
// move eagerly: every name the old address owns (at most
// MaxNamesPerAddress), its OwnerInfo with primary_name, display_name and
// the scavenging clock, and every name whose target is the old address.
type MigrationHooks struct {
	k *Keeper
}

// NewMigrationHooks returns the x/name migration hooks.
func NewMigrationHooks(k *Keeper) MigrationHooks {
	return MigrationHooks{k: k}
}

var _ reptypes.MemberMigrationHooks = MigrationHooks{}

// BeforeMemberMigration refuses a KEY migration while the old address owns a
// name under an ACTIVE dispute (the owner settles it first; a resolved
// dispute record is history and never blocks), and refuses a target that
// already owns names or has an owner record beyond a bare display name
// (freshness, plan §7.1).
//
// On the RECOVERY and COUNCIL paths an active dispute does not veto: the
// owner has lost the key and cannot settle it, and any member can file a
// dispute, so a veto would let a griefer block recovery for one dispute
// stake. The dispute is keyed by name and follows it to the new owner;
// contest and resolution read ownership through the alias contract.

func (h MigrationHooks) BeforeMemberMigration(ctx context.Context, from, to sdk.AccAddress, path reptypes.MigrationPath) error {
	k := h.k
	fromStr, toStr := from.String(), to.String()
	// A display-name-only OwnerInfo (MsgSetDisplayName needs no names and
	// no membership) still counts as fresh: refusing it would let a
	// recovery key that once set a display name block its own migration.
	if info, err := k.Owners.Get(ctx, toStr); err == nil && info.PrimaryName != "" {
		return fmt.Errorf("x/name: %s already has an owner record", toStr)
	}
	if n, _ := k.GetOwnedNamesCount(ctx, to); n > 0 {
		return fmt.Errorf("x/name: %s already owns %d name(s)", toStr, n)
	}
	if path != reptypes.MigrationPath_MIGRATION_PATH_KEY {
		return nil
	}
	// Check the old address's own names (at most MaxNamesPerAddress)
	// instead of walking every dispute ever filed (NAM-05).
	var disputed string
	rng := collections.NewPrefixedPairRange[string, string](fromStr)
	if err := k.OwnerNames.Walk(ctx, rng, func(key collections.Pair[string, string]) (bool, error) {
		if d, found := k.GetDispute(ctx, key.K2()); found && d.Active {
			disputed = key.K2()
			return true, nil
		}
		return false, nil
	}); err != nil {
		return err
	}
	if disputed != "" {
		return fmt.Errorf("x/name: %s owns %q, which is under dispute", fromStr, disputed)
	}
	return nil
}

// AfterMemberMigration moves names, the owner record and forward targets.
func (h MigrationHooks) AfterMemberMigration(ctx context.Context, from, to sdk.AccAddress, path reptypes.MigrationPath) error {
	k := h.k
	fromStr, toStr := from.String(), to.String()

	var owned []string
	rng := collections.NewPrefixedPairRange[string, string](fromStr)
	if err := k.OwnerNames.Walk(ctx, rng, func(key collections.Pair[string, string]) (bool, error) {
		owned = append(owned, key.K2())
		return false, nil
	}); err != nil {
		return err
	}
	for _, name := range owned {
		if err := k.OwnerNames.Remove(ctx, collections.Join(fromStr, name)); err != nil {
			return err
		}
		if err := k.OwnerNames.Set(ctx, collections.Join(toStr, name)); err != nil {
			return err
		}
		record, found := k.GetName(ctx, name)
		if !found {
			continue
		}
		record.Owner = toStr
		if err := k.SetName(ctx, record); err != nil {
			return err
		}
	}

	if info, err := k.Owners.Get(ctx, fromStr); err == nil {
		if err := k.Owners.Remove(ctx, fromStr); err != nil {
			return err
		}
		info.Address = toStr
		if err := k.Owners.Set(ctx, toStr, info); err != nil {
			return err
		}
	}

	// Names pointing at the old address: repoint the target and the
	// accepted-target consent, which was given by the same identity.
	// Read through the TargetNames index rather than the whole registry.
	var targeted []types.NameRecord
	trng := collections.NewPrefixedPairRange[string, string](fromStr)
	if err := k.TargetNames.Walk(ctx, trng, func(key collections.Pair[string, string]) (bool, error) {
		if r, found := k.GetName(ctx, key.K2()); found && r.Target == fromStr {
			targeted = append(targeted, r)
		}
		return false, nil
	}); err != nil {
		return err
	}
	for _, r := range targeted {
		if r.TargetAccepted {
			if err := k.AcceptedTargets.Remove(ctx, collections.Join(fromStr, r.Name)); err != nil {
				return err
			}
			if err := k.AcceptedTargets.Set(ctx, collections.Join(toStr, r.Name)); err != nil {
				return err
			}
		}
		r.Target = toStr
		if err := k.SetName(ctx, r); err != nil {
			return err
		}
	}

	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent("names_migrated",
		sdk.NewAttribute("from", fromStr),
		sdk.NewAttribute("to", toStr),
		sdk.NewAttribute("names", fmt.Sprintf("%d", len(owned))),
		sdk.NewAttribute("targets", fmt.Sprintf("%d", len(targeted))),
	))
	return nil
}

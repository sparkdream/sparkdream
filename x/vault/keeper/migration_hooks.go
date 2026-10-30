package keeper

import (
	"context"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"

	reptypes "sparkdream/x/rep/types"
	"sparkdream/x/vault/types"
)

// MigrationHooks implements x/rep's MemberMigrationHooks for x/vault.
//
// Vault signers are never re-keyed: a signer entry stays under the old
// address and is read through the alias contract, so a migration moves the
// signer's weight to the new address on every path. On the KEY path a target
// that already co-signs a vault with the source is refused: the two entries
// would merge into one member while the other signers still see two people,
// and the owner can rotate one of them out first. On the RECOVERY and
// COUNCIL paths the hook does not veto: a thief holding the old key could
// otherwise block a recovery with a vault where the old key co-signs with the
// new one (REP-R-02). There Tally counts the merged member once, at its
// largest entry weight. Only accepted entries matter: an unaccepted entry has
// no weight, and AcceptSigner refuses an entry whose member already holds an
// accepted entry in the same vault, so a merge through an invitation can
// never count twice.
type MigrationHooks struct {
	k *Keeper
}

// NewMigrationHooks returns the x/vault migration hooks.
func NewMigrationHooks(k *Keeper) MigrationHooks {
	return MigrationHooks{k: k}
}

var _ reptypes.MemberMigrationHooks = MigrationHooks{}

// BeforeMemberMigration refuses a target that co-signs a vault with from
// (both entries accepted). It walks the target's accepted vaults, not the
// source's: the target is a fresh
// address (x/rep's freshness rule), so it has no aliases and at most
// MaxVaultsPerSigner entries, while the source's entries can grow with its
// alias history. The common case, a target that signs no vault, costs one
// empty prefix walk. Bounded by MaxVaultsPerSigner vaults × MaxSigners signers.
func (h MigrationHooks) BeforeMemberMigration(ctx context.Context, from, to sdk.AccAddress, path reptypes.MigrationPath) error {
	if path != reptypes.MigrationPath_MIGRATION_PATH_KEY {
		return nil
	}
	var ids []uint64
	seen := map[uint64]bool{}
	for _, a := range h.k.allAddressesOf(ctx, to.String()) {
		rng := collections.NewPrefixedPairRange[string, uint64](a)
		if err := h.k.VaultsBySigner.Walk(ctx, rng, func(key collections.Pair[string, uint64]) (bool, error) {
			if !seen[key.K2()] {
				seen[key.K2()] = true
				ids = append(ids, key.K2())
			}
			return false, nil
		}); err != nil {
			return err
		}
	}
	if len(ids) == 0 {
		return nil
	}
	fromStr := from.String()
	fromCanon := h.k.canonical(ctx, fromStr)
	for _, id := range ids {
		v, err := h.k.Vault.Get(ctx, id)
		if err != nil {
			continue
		}
		for _, s := range v.Signers {
			if !s.Accepted {
				continue
			}
			if s.Address == fromStr || h.k.canonical(ctx, s.Address) == fromCanon {
				return errorsmod.Wrapf(types.ErrInvalidSignerSet,
					"x/vault: %s already co-signs vault %d with %s", to.String(), id, fromStr)
			}
		}
	}
	return nil
}

// AfterMemberMigration does nothing: vault signers are read through the alias.
func (h MigrationHooks) AfterMemberMigration(context.Context, sdk.AccAddress, sdk.AccAddress, reptypes.MigrationPath) error {
	return nil
}

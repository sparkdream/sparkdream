package keeper

import (
	"context"
	"fmt"

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

// AfterMemberMigration leaves signer entries alone (they are read through
// the alias) on every path. On the RECOVERY and COUNCIL paths the old key
// is presumed stolen (VLT-102), so whatever it did in a still-pending
// proposal stops counting: proposals it opened are cancelled and its
// approvals are dropped. Approvals are stored under the approver's
// canonical address at approval time, which for the old key was from
// itself. The recovered member re-approves with the new key. The KEY path
// keeps both: the member moved deliberately. On every path a vault whose
// entries the migration merged into one member (two entries, one person,
// counted once) has its threshold clamped to the reachable weight, so a
// unanimity vault stays a unanimity vault instead of freezing (§7); the
// clamp bumps config_version and invalidates the vault's pending proposals.
// Bounded by MaxVaultsPerSigner vaults × MaxSigners × the per-proposer
// pending caps (counted across every alias of a proposer, so the alias
// history adds no factor).
func (h MigrationHooks) AfterMemberMigration(ctx context.Context, from, to sdk.AccAddress, path reptypes.MigrationPath) error {
	k := h.k
	fromStr := from.String()
	stolen := path != reptypes.MigrationPath_MIGRATION_PATH_KEY
	var vaultIDs []uint64
	seen := map[uint64]bool{}
	collect := func(key collections.Pair[string, uint64]) (bool, error) {
		if !seen[key.K2()] {
			seen[key.K2()] = true
			vaultIDs = append(vaultIDs, key.K2())
		}
		return false, nil
	}
	for _, a := range append(k.allAddressesOf(ctx, fromStr), k.allAddressesOf(ctx, to.String())...) {
		rng := collections.NewPrefixedPairRange[string, uint64](a)
		if err := k.VaultsBySigner.Walk(ctx, rng, collect); err != nil {
			return err
		}
		if err := k.VaultInvitesBySigner.Walk(ctx, rng, collect); err != nil {
			return err
		}
	}
	for _, vaultID := range vaultIDs {
		if stolen {
			var pending []uint64
			rng := collections.NewPrefixedPairRange[uint64, uint64](vaultID)
			if err := k.PendingByVault.Walk(ctx, rng, func(key collections.Pair[uint64, uint64]) (bool, error) {
				pending = append(pending, key.K2())
				return false, nil
			}); err != nil {
				return err
			}
			for _, id := range pending {
				if err := k.dropMigratedApprovals(ctx, id, fromStr); err != nil {
					return err
				}
			}
		}
		v, err := k.Vault.Get(ctx, vaultID)
		if err != nil {
			return err
		}
		if k.clampThreshold(ctx, &v, "migration_merge") {
			if err := k.storeConfigChange(ctx, &v, true); err != nil {
				return err
			}
		}
	}
	return nil
}

// dropMigratedApprovals cancels a pending proposal opened by the stolen key
// from, or removes from's approval from it.
func (k Keeper) dropMigratedApprovals(ctx context.Context, id uint64, from string) error {
	p, err := k.VaultProposal.Get(ctx, id)
	if err != nil {
		return err
	}
	if p.Status != types.ProposalStatus_PROPOSAL_STATUS_PENDING {
		return nil
	}
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	if p.Proposer == from {
		if err := k.resolveProposal(ctx, p, types.ProposalStatus_PROPOSAL_STATUS_CANCELLED); err != nil {
			return err
		}
		sdkCtx.EventManager().EmitEvent(sdk.NewEvent("vault_proposal_cancelled",
			sdk.NewAttribute("proposal_id", fmt.Sprintf("%d", id)),
			sdk.NewAttribute("by", "migration"),
		))
		return nil
	}
	kept := make([]string, 0, len(p.Approvals))
	for _, a := range p.Approvals {
		if a != from {
			kept = append(kept, a)
		}
	}
	if len(kept) == len(p.Approvals) {
		return nil
	}
	p.Approvals = kept
	if err := k.VaultProposal.Set(ctx, id, p); err != nil {
		return err
	}
	sdkCtx.EventManager().EmitEvent(sdk.NewEvent("vault_approval_revoked",
		sdk.NewAttribute("proposal_id", fmt.Sprintf("%d", id)),
		sdk.NewAttribute("approver", from),
		sdk.NewAttribute("by", "migration"),
	))
	return nil
}

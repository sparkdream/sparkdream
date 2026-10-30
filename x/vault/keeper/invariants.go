package keeper

import (
	"fmt"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/vault/types"
)

// RegisterInvariants registers the x/vault invariants (run by app's
// TestInvariants, x/crisis is not wired).
func RegisterInvariants(ir sdk.InvariantRegistry, k Keeper) {
	ir.RegisterRoute(types.ModuleName, "vault-indexes", VaultIndexesInvariant(k))
	ir.RegisterRoute(types.ModuleName, "proposal-indexes", ProposalIndexesInvariant(k))
}

// VaultIndexesInvariant: VaultByAddress, VaultsBySigner (accepted entries)
// and VaultInvitesBySigner (unaccepted entries) agree with Vaults; every
// vault's threshold is reachable and its address derives from its id.
func VaultIndexesInvariant(k Keeper) sdk.Invariant {
	return func(ctx sdk.Context) (string, bool) {
		var problems []string
		vaults := 0
		signerEntries := 0
		inviteEntries := 0
		if err := k.Vault.Walk(ctx, nil, func(id uint64, v types.Vault) (bool, error) {
			vaults++
			if got, err := k.addressCodec.BytesToString(types.VaultAddress(id)); err != nil || got != v.Address {
				problems = append(problems, fmt.Sprintf("vault %d address %s does not derive from its id", id, v.Address))
			}
			if idx, err := k.VaultByAddress.Get(ctx, v.Address); err != nil || idx != id {
				problems = append(problems, fmt.Sprintf("vault %d missing from VaultByAddress", id))
			}
			var sum uint64
			for _, s := range v.Signers {
				sum += s.Weight
				if s.Accepted {
					signerEntries++
				} else {
					inviteEntries++
				}
				if has, _ := k.signerIndex(s).Has(ctx, collections.Join(s.Address, id)); !has {
					problems = append(problems, fmt.Sprintf("vault %d signer %s (accepted=%t) missing from its index", id, s.Address, s.Accepted))
				}
			}
			if v.Threshold == 0 || v.Threshold > sum {
				problems = append(problems, fmt.Sprintf("vault %d threshold %d unreachable (sum %d)", id, v.Threshold, sum))
			}
			return false, nil
		}); err != nil {
			return fmt.Sprintf("Vault.Walk failed: %v", err), true
		}
		byAddr := 0
		if err := k.VaultByAddress.Walk(ctx, nil, func(addr string, id uint64) (bool, error) {
			byAddr++
			if v, err := k.Vault.Get(ctx, id); err != nil || v.Address != addr {
				problems = append(problems, fmt.Sprintf("VaultByAddress %s -> %d dangling", addr, id))
			}
			return false, nil
		}); err != nil {
			return fmt.Sprintf("VaultByAddress.Walk failed: %v", err), true
		}
		bySigner := 0
		if err := k.VaultsBySigner.Walk(ctx, nil, func(key collections.Pair[string, uint64]) (bool, error) {
			bySigner++
			if v, err := k.Vault.Get(ctx, key.K2()); err != nil {
				problems = append(problems, fmt.Sprintf("VaultsBySigner (%s,%d) dangling", key.K1(), key.K2()))
			} else if s, ok := v.SignerEntry(key.K1()); !ok || !s.Accepted {
				problems = append(problems, fmt.Sprintf("VaultsBySigner (%s,%d) is not an accepted entry", key.K1(), key.K2()))
			}
			return false, nil
		}); err != nil {
			return fmt.Sprintf("VaultsBySigner.Walk failed: %v", err), true
		}
		byInvite := 0
		if err := k.VaultInvitesBySigner.Walk(ctx, nil, func(key collections.Pair[string, uint64]) (bool, error) {
			byInvite++
			if v, err := k.Vault.Get(ctx, key.K2()); err != nil {
				problems = append(problems, fmt.Sprintf("VaultInvitesBySigner (%s,%d) dangling", key.K1(), key.K2()))
			} else if s, ok := v.SignerEntry(key.K1()); !ok || s.Accepted {
				problems = append(problems, fmt.Sprintf("VaultInvitesBySigner (%s,%d) is not an unaccepted entry", key.K1(), key.K2()))
			}
			return false, nil
		}); err != nil {
			return fmt.Sprintf("VaultInvitesBySigner.Walk failed: %v", err), true
		}
		if byAddr != vaults {
			problems = append(problems, fmt.Sprintf("VaultByAddress has %d entries, Vaults has %d", byAddr, vaults))
		}
		if bySigner != signerEntries {
			problems = append(problems, fmt.Sprintf("VaultsBySigner has %d entries, vaults list %d accepted signers", bySigner, signerEntries))
		}
		if byInvite != inviteEntries {
			problems = append(problems, fmt.Sprintf("VaultInvitesBySigner has %d entries, vaults list %d unaccepted signers", byInvite, inviteEntries))
		}
		return sdk.FormatInvariant(types.ModuleName, "vault-indexes", fmt.Sprintf("%d problems: %v", len(problems), problems)), len(problems) > 0
	}
}

// ProposalIndexesInvariant: every pending index and the expiry queue point at
// PENDING proposals and agree with each other; the retention queue points at
// resolved ones; every proposal belongs to an existing vault.
func ProposalIndexesInvariant(k Keeper) sdk.Invariant {
	return func(ctx sdk.Context) (string, bool) {
		var problems []string
		pending := 0
		resolved := 0
		if err := k.VaultProposal.Walk(ctx, nil, func(id uint64, p types.VaultProposal) (bool, error) {
			if _, err := k.Vault.Get(ctx, p.VaultId); err != nil {
				problems = append(problems, fmt.Sprintf("proposal %d belongs to missing vault %d", id, p.VaultId))
			}
			if p.Status == types.ProposalStatus_PROPOSAL_STATUS_PENDING {
				pending++
				if has, _ := k.PendingByVault.Has(ctx, collections.Join(p.VaultId, id)); !has {
					problems = append(problems, fmt.Sprintf("pending proposal %d missing from PendingByVault", id))
				}
				if has, _ := k.PendingByProposer.Has(ctx, collections.Join3(p.VaultId, p.Proposer, id)); !has {
					problems = append(problems, fmt.Sprintf("pending proposal %d missing from PendingByProposer", id))
				}
				if has, _ := k.ExpiryQueue.Has(ctx, collections.Join(p.ExpiresAt, id)); !has {
					problems = append(problems, fmt.Sprintf("pending proposal %d missing from ExpiryQueue", id))
				}
			} else {
				resolved++
				if has, _ := k.RetentionQueue.Has(ctx, collections.Join(p.ResolvedAt, id)); !has {
					problems = append(problems, fmt.Sprintf("resolved proposal %d missing from RetentionQueue", id))
				}
			}
			return false, nil
		}); err != nil {
			return fmt.Sprintf("VaultProposal.Walk failed: %v", err), true
		}
		count := func(name string, n int, want int) {
			if n != want {
				problems = append(problems, fmt.Sprintf("%s has %d entries, want %d", name, n, want))
			}
		}
		n := 0
		if err := k.PendingByVault.Walk(ctx, nil, func(key collections.Pair[uint64, uint64]) (bool, error) {
			n++
			if p, err := k.VaultProposal.Get(ctx, key.K2()); err != nil || p.Status != types.ProposalStatus_PROPOSAL_STATUS_PENDING || p.VaultId != key.K1() {
				problems = append(problems, fmt.Sprintf("PendingByVault (%d,%d) does not point at a PENDING proposal", key.K1(), key.K2()))
			}
			return false, nil
		}); err != nil {
			return fmt.Sprintf("PendingByVault.Walk failed: %v", err), true
		}
		count("PendingByVault", n, pending)
		n = 0
		if err := k.PendingByProposer.Walk(ctx, nil, func(key collections.Triple[uint64, string, uint64]) (bool, error) {
			n++
			if p, err := k.VaultProposal.Get(ctx, key.K3()); err != nil || p.Status != types.ProposalStatus_PROPOSAL_STATUS_PENDING || p.Proposer != key.K2() {
				problems = append(problems, fmt.Sprintf("PendingByProposer (%d,%s,%d) does not point at a PENDING proposal", key.K1(), key.K2(), key.K3()))
			}
			return false, nil
		}); err != nil {
			return fmt.Sprintf("PendingByProposer.Walk failed: %v", err), true
		}
		count("PendingByProposer", n, pending)
		n = 0
		if err := k.ExpiryQueue.Walk(ctx, nil, func(key collections.Pair[int64, uint64]) (bool, error) {
			n++
			if p, err := k.VaultProposal.Get(ctx, key.K2()); err != nil || p.Status != types.ProposalStatus_PROPOSAL_STATUS_PENDING || p.ExpiresAt != key.K1() {
				problems = append(problems, fmt.Sprintf("ExpiryQueue (%d,%d) does not point at a PENDING proposal", key.K1(), key.K2()))
			}
			return false, nil
		}); err != nil {
			return fmt.Sprintf("ExpiryQueue.Walk failed: %v", err), true
		}
		count("ExpiryQueue", n, pending)
		n = 0
		if err := k.RetentionQueue.Walk(ctx, nil, func(key collections.Pair[int64, uint64]) (bool, error) {
			n++
			if p, err := k.VaultProposal.Get(ctx, key.K2()); err != nil || p.Status == types.ProposalStatus_PROPOSAL_STATUS_PENDING || p.ResolvedAt != key.K1() {
				problems = append(problems, fmt.Sprintf("RetentionQueue (%d,%d) does not point at a resolved proposal", key.K1(), key.K2()))
			}
			return false, nil
		}); err != nil {
			return fmt.Sprintf("RetentionQueue.Walk failed: %v", err), true
		}
		count("RetentionQueue", n, resolved)
		return sdk.FormatInvariant(types.ModuleName, "proposal-indexes", fmt.Sprintf("%d problems: %v", len(problems), problems)), len(problems) > 0
	}
}

package simulation

import (
	"math/rand"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/vault/keeper"
	"sparkdream/x/vault/types"
)

// maxScan bounds how many records a sim op looks at before giving up.
const maxScan = 32

func findAccount(accs []simtypes.Account, addr string) (simtypes.Account, bool) {
	for _, a := range accs {
		if a.Address.String() == addr {
			return a, true
		}
	}
	return simtypes.Account{}, false
}

// randomVault returns a random vault among the first maxScan, or false.
func randomVault(r *rand.Rand, ctx sdk.Context, k keeper.Keeper) (types.Vault, bool) {
	var vaults []types.Vault
	_ = k.Vault.Walk(ctx, nil, func(_ uint64, v types.Vault) (bool, error) {
		vaults = append(vaults, v)
		return len(vaults) >= maxScan, nil
	})
	if len(vaults) == 0 {
		return types.Vault{}, false
	}
	return vaults[r.Intn(len(vaults))], true
}

// randomPendingProposal returns a random PENDING proposal (with its vault) that
// is still valid for its vault's config version.
func randomPendingProposal(r *rand.Rand, ctx sdk.Context, k keeper.Keeper) (types.Vault, types.VaultProposal, bool) {
	var ids []uint64
	_ = k.PendingByVault.Walk(ctx, nil, func(key collections.Pair[uint64, uint64]) (bool, error) {
		ids = append(ids, key.K2())
		return len(ids) >= maxScan, nil
	})
	r.Shuffle(len(ids), func(i, j int) { ids[i], ids[j] = ids[j], ids[i] })
	now := ctx.BlockTime().Unix()
	for _, id := range ids {
		p, err := k.VaultProposal.Get(ctx, id)
		if err != nil || p.Status != types.ProposalStatus_PROPOSAL_STATUS_PENDING || now >= p.ExpiresAt {
			continue
		}
		v, err := k.Vault.Get(ctx, p.VaultId)
		if err != nil || v.ConfigVersion != p.ConfigVersion {
			continue
		}
		return v, p, true
	}
	return types.Vault{}, types.VaultProposal{}, false
}

// signerAccounts lists the vault's accepted signers that are simulation
// accounts (only accepted entries may propose, approve or execute).
func signerAccounts(v types.Vault, accs []simtypes.Account) []simtypes.Account {
	var out []simtypes.Account
	for _, s := range v.Signers {
		if !s.Accepted {
			continue
		}
		if a, ok := findAccount(accs, s.Address); ok {
			out = append(out, a)
		}
	}
	return out
}

// invitedAccounts lists the vault's unaccepted signers that are simulation
// accounts.
func invitedAccounts(v types.Vault, accs []simtypes.Account) []simtypes.Account {
	var out []simtypes.Account
	for _, s := range v.Signers {
		if s.Accepted {
			continue
		}
		if a, ok := findAccount(accs, s.Address); ok {
			out = append(out, a)
		}
	}
	return out
}

func hasApproved(p types.VaultProposal, addr string) bool {
	for _, a := range p.Approvals {
		if a == addr {
			return true
		}
	}
	return false
}

// acceptedVaults counts the vaults addr is an accepted signer of (no alias
// expansion: simulation accounts never migrate), stopping at the cap.
func acceptedVaults(ctx sdk.Context, k keeper.Keeper, addr string) int {
	n := 0
	_ = k.VaultsBySigner.Walk(ctx, collections.NewPrefixedPairRange[string, uint64](addr), func(collections.Pair[string, uint64]) (bool, error) {
		n++
		return n >= types.MaxVaultsPerSigner, nil
	})
	return n
}

package keeper

import (
	"context"

	"cosmossdk.io/collections"

	"sparkdream/x/vault/types"
)

// InitGenesis initializes the module's state from a provided genesis state.
func (k Keeper) InitGenesis(ctx context.Context, genState types.GenesisState) error {
	// Indexes, queues and sequences are derived from the two value maps.
	var maxVault, maxProposal uint64
	for _, elem := range genState.VaultMap {
		if err := k.Vault.Set(ctx, elem.VaultId, elem); err != nil {
			return err
		}
		if err := k.VaultByAddress.Set(ctx, elem.Address, elem.VaultId); err != nil {
			return err
		}
		for _, s := range elem.Signers {
			if err := k.VaultsBySigner.Set(ctx, collections.Join(s.Address, elem.VaultId)); err != nil {
				return err
			}
		}
		if elem.VaultId > maxVault {
			maxVault = elem.VaultId
		}
	}
	for _, elem := range genState.VaultProposalMap {
		if err := k.VaultProposal.Set(ctx, elem.ProposalId, elem); err != nil {
			return err
		}
		if err := k.indexProposal(ctx, elem); err != nil {
			return err
		}
		if elem.ProposalId > maxProposal {
			maxProposal = elem.ProposalId
		}
	}
	if err := k.VaultSeq.Set(ctx, maxVault); err != nil {
		return err
	}
	if err := k.ProposalSeq.Set(ctx, maxProposal); err != nil {
		return err
	}

	return k.Params.Set(ctx, genState.Params)
}

// ExportGenesis returns the module's exported genesis.
func (k Keeper) ExportGenesis(ctx context.Context) (*types.GenesisState, error) {
	var err error

	genesis := types.DefaultGenesis()
	genesis.Params, err = k.Params.Get(ctx)
	if err != nil {
		return nil, err
	}
	if err := k.Vault.Walk(ctx, nil, func(_ uint64, val types.Vault) (stop bool, err error) {
		genesis.VaultMap = append(genesis.VaultMap, val)
		return false, nil
	}); err != nil {
		return nil, err
	}
	if err := k.VaultProposal.Walk(ctx, nil, func(_ uint64, val types.VaultProposal) (stop bool, err error) {
		genesis.VaultProposalMap = append(genesis.VaultProposalMap, val)
		return false, nil
	}); err != nil {
		return nil, err
	}

	return genesis, nil
}

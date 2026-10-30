package keeper

import (
	"context"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/vault/types"
)

// InitGenesis initializes the module's state from a provided genesis state.
func (k Keeper) InitGenesis(ctx context.Context, genState types.GenesisState) error {
	// Indexes, queues and sequences are derived from the two value maps.
	// Validate is re-run here: InitGenesis is also reached from paths that
	// skip ValidateGenesis (exported-genesis merges, tests).
	if err := genState.Validate(); err != nil {
		return err
	}
	var maxVault, maxProposal uint64
	for _, elem := range genState.VaultMap {
		want, err := k.addressCodec.BytesToString(types.VaultAddress(elem.VaultId))
		if err != nil {
			return err
		}
		if elem.Address != want {
			return fmt.Errorf("vault %d: address %s, want %s", elem.VaultId, elem.Address, want)
		}
		if err := k.Vault.Set(ctx, elem.VaultId, elem); err != nil {
			return err
		}
		if err := k.VaultByAddress.Set(ctx, elem.Address, elem.VaultId); err != nil {
			return err
		}
		// Accepted entries go to VaultsBySigner, unaccepted ones to
		// VaultInvitesBySigner; the flag is exported with the vault.
		if err := k.indexSigners(ctx, elem.VaultId, elem.Signers); err != nil {
			return err
		}
		if elem.VaultId > maxVault {
			maxVault = elem.VaultId
		}
	}
	now := sdk.UnwrapSDKContext(ctx).BlockTime().Unix()
	for _, elem := range genState.VaultProposalMap {
		if elem.Status == types.ProposalStatus_PROPOSAL_STATUS_PENDING {
			// A PENDING record must be one Propose could have written: its
			// messages unpack, pass the denylist and are signed by the vault,
			// and its expiry is within one MaxProposalTTL of genesis (a
			// far-future expiry would hold a proposer cap slot forever).
			v, err := k.Vault.Get(ctx, elem.VaultId)
			if err != nil {
				return fmt.Errorf("vaultProposal %d: vault %d: %w", elem.ProposalId, elem.VaultId, err)
			}
			if _, err := k.checkInnerMsgs(ctx, v, elem.Messages); err != nil {
				return fmt.Errorf("vaultProposal %d: %w", elem.ProposalId, err)
			}
			if now > 0 && elem.ExpiresAt > now+types.MaxProposalTTL {
				return fmt.Errorf("vaultProposal %d: expires_at %d is more than MaxProposalTTL after genesis time %d", elem.ProposalId, elem.ExpiresAt, now)
			}
		}
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
	// The exported sequences win over the highest live ID (pruned
	// proposals can sit above it); older genesis files without them fall
	// back to the highest live ID.
	if err := k.VaultSeq.Set(ctx, max(maxVault, genState.VaultSeq)); err != nil {
		return err
	}
	if err := k.ProposalSeq.Set(ctx, max(maxProposal, genState.ProposalSeq)); err != nil {
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
	if genesis.VaultSeq, err = k.VaultSeq.Peek(ctx); err != nil {
		return nil, err
	}
	if genesis.ProposalSeq, err = k.ProposalSeq.Peek(ctx); err != nil {
		return nil, err
	}

	return genesis, nil
}

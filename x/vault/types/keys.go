package types

import "cosmossdk.io/collections"

const (
	// ModuleName defines the module name
	ModuleName = "vault"

	// StoreKey defines the primary module store key
	StoreKey = ModuleName

	// GovModuleName duplicates the gov module's name to avoid a dependency with x/gov.
	// It should be synced with the gov module's name if it is ever changed.
	// See: https://github.com/cosmos/cosmos-sdk/blob/v0.52.0-beta.2/x/gov/types/keys.go#L9
	GovModuleName = "gov"
)

// ParamsKey is the prefix to retrieve all Params
var ParamsKey = collections.NewPrefix("p_vault")

var (
	// VaultKey and VaultProposalKey live in key_vault.go / key_vault_proposal.go.
	// VaultSeqKey / ProposalSeqKey hold the next ids.
	VaultSeqKey    = collections.NewPrefix("vault/seq/")
	ProposalSeqKey = collections.NewPrefix("vaultProposal/seq/")
	// VaultByAddressKey indexes address -> vault_id.
	VaultByAddressKey = collections.NewPrefix("vault/by_address/")
	// VaultsBySignerKey indexes (signer, vault_id); never iterated in full.
	VaultsBySignerKey = collections.NewPrefix("vault/by_signer/")
	// PendingByVaultKey indexes (vault_id, proposal_id) of PENDING proposals.
	PendingByVaultKey = collections.NewPrefix("vaultProposal/pending_by_vault/")
	// PendingByProposerKey indexes (vault_id, proposer, proposal_id) of PENDING proposals.
	PendingByProposerKey = collections.NewPrefix("vaultProposal/pending_by_proposer/")
	// ExpiryQueueKey indexes (expires_at, proposal_id) of PENDING proposals.
	ExpiryQueueKey = collections.NewPrefix("vaultProposal/expiry/")
	// RetentionQueueKey indexes (resolved_at, proposal_id) of resolved proposals.
	RetentionQueueKey = collections.NewPrefix("vaultProposal/retention/")
)

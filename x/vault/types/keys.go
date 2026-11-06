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
	// VaultsBySignerKey indexes (signer, vault_id) of accepted signer
	// entries; never iterated in full.
	VaultsBySignerKey = collections.NewPrefix("vault/by_signer/")
	// VaultInvitesBySignerKey indexes (signer, vault_id) of unaccepted
	// signer entries; never iterated in full.
	VaultInvitesBySignerKey = collections.NewPrefix("vault/invites_by_signer/")
	// InviteExpiryQueueKey indexes (invite_expires_at, vault_id, signer) of
	// unaccepted entries with a non-zero expiry.
	InviteExpiryQueueKey = collections.NewPrefix("vault/invite_expiry/")
	// ProposalsByVaultKey indexes (vault_id, proposal_id) of every stored proposal.
	ProposalsByVaultKey = collections.NewPrefix("vaultProposal/by_vault/")
	// ProposalsByProposerKey indexes (proposer, proposal_id) of every stored proposal.
	ProposalsByProposerKey = collections.NewPrefix("vaultProposal/by_proposer/")
	// PendingByVaultKey indexes (vault_id, proposal_id) of PENDING proposals.
	PendingByVaultKey = collections.NewPrefix("vaultProposal/pending_by_vault/")
	// PendingByProposerKey indexes (vault_id, proposer, proposal_id) of PENDING proposals.
	PendingByProposerKey = collections.NewPrefix("vaultProposal/pending_by_proposer/")
	// ExpiryQueueKey indexes (expires_at, proposal_id) of PENDING proposals.
	ExpiryQueueKey = collections.NewPrefix("vaultProposal/expiry/")
	// RetentionQueueKey indexes (resolved_at, proposal_id) of resolved proposals.
	RetentionQueueKey = collections.NewPrefix("vaultProposal/retention/")
)

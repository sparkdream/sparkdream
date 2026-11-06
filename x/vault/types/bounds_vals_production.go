//go:build mainnet || testnet || devnet

package types

// Production windows (docs/x-vault-spec.md).
const (
	// MinProposalTTL is the shortest proposal lifetime a vault may configure.
	MinProposalTTL int64 = 3600
	// ProposalRetention is how long a resolved proposal stays queryable.
	ProposalRetention int64 = 7 * 24 * 3600
	// InviteTTL is how long an unaccepted signer listing waits for
	// MsgAcceptVaultSigner before the EndBlocker removes it.
	InviteTTL int64 = 30 * 24 * 3600
)

//go:build !mainnet && !testnet && !devnet

package types

// Test-build windows: seconds instead of hours/days so test/vault can watch a
// proposal expire and a resolved one be pruned within one run.
const (
	MinProposalTTL    int64 = 10
	ProposalRetention int64 = 60
)

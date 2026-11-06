//go:build mainnet || testnet || devnet

package types

// PendingGrantTTLSeconds is how long a PENDING access-class grant waits for
// the grantee's acceptance before the EndBlocker drops it and forfeits the
// granter's deposit (docs/x-session-spec.md §31). A grant whose own
// expires_at is earlier expires then instead.
const PendingGrantTTLSeconds int64 = 24 * 3600

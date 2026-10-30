//go:build testnet

package types

// hiddenExpirationDefault on testnet matches mainnet (7 days) so the appeal
// flow runs against realistic windows.
func hiddenExpirationDefault() int64 { return 604800 } // 7 days

// opsClockFloorsEnabled applies the lower clock bounds of
// ValidateCommitteeBounds (network builds only).
const opsClockFloorsEnabled = true

// hideAppealCooldownDefault: 1 hour.
func hideAppealCooldownDefault() int64 { return 3600 }

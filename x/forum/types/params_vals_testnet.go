//go:build testnet

package types

// hiddenExpirationDefault on testnet matches mainnet (7 days) so the appeal
// flow runs against realistic windows.
func hiddenExpirationDefault() int64 { return 604800 } // 7 days

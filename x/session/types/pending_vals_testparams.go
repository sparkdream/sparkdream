//go:build !mainnet && !testnet && !devnet

package types

// PendingGrantTTLSeconds in the test build: long enough for an e2e script
// to create a session key and exec it after its other setup, short enough
// to watch an unaccepted grant expire within one run.
const PendingGrantTTLSeconds int64 = 180

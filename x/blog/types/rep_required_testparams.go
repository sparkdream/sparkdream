//go:build !mainnet && !testnet && !devnet

package types

// RepKeeperRequired is off in the test build: standalone keeper unit tests
// run without a rep keeper and take the documented fallbacks.
const RepKeeperRequired = false

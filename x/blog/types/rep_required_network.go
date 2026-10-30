//go:build mainnet || testnet || devnet

package types

// RepKeeperRequired: network builds treat a missing (unwired) rep keeper as
// a fatal wiring bug instead of skipping the rep side of an operation.
const RepKeeperRequired = true

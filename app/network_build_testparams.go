//go:build !mainnet && !testnet && !devnet

package app

// builtForNetwork is empty for untagged (testparams) builds.
const builtForNetwork = ""

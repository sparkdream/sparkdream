//go:build mainnet || testnet || devnet

package keeper

// requireVerificationKey returns true in production builds, ensuring all
// shielded operations require a valid VK to be registered. Without a VK,
// proof verification is impossible and MsgShieldedExec is rejected.
func requireVerificationKey() bool {
	return true
}

//go:build !mainnet && !testnet && !devnet

package keeper

// requireVerificationKey returns false in test mode, allowing shielded
// operations to proceed without a registered VK. This enables E2E tests
// to exercise the full MsgShieldedExec flow with dummy proofs.
//
// In production builds (mainnet/testnet/devnet), this returns true and
// all shielded operations require a valid VK to be registered first.
func requireVerificationKey() bool {
	return false
}

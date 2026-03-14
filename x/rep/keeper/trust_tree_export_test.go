package keeper

import zkcrypto "sparkdream/tools/crypto"

// SetTestTreeDepth sets a small tree depth for testing and reinitializes zero hashes.
func SetTestTreeDepth(depth int) {
	trustTreeDepth = depth
	initZeroHashes(depth)
}

// RestoreTreeDepth restores the production tree depth and zero hashes.
func RestoreTreeDepth() {
	trustTreeDepth = zkcrypto.TreeDepth
	initZeroHashes(zkcrypto.TreeDepth)
}

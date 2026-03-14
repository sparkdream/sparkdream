package keeper

import (
	"context"
)

// GetTrustTreeRoot returns the current trust tree Merkle root.
// Used by x/shield for ZK proof root validation (PROOF_DOMAIN_TRUST_TREE).
//
// All anonymous operations use the trust tree. The unified ShieldCircuit proves
// membership + "trustLevel >= minTrustLevel" without revealing the exact level,
// making a separate voter tree unnecessary.
func (k Keeper) GetTrustTreeRoot(ctx context.Context) ([]byte, error) {
	return k.GetMemberTrustTreeRoot(ctx)
}

// GetPreviousTrustTreeRoot returns the previous trust tree Merkle root.
// Used by x/shield to accept proofs generated against slightly stale roots.
func (k Keeper) GetPreviousTrustTreeRoot(ctx context.Context) ([]byte, error) {
	root := k.GetPreviousMemberTrustTreeRoot(ctx)
	return root, nil
}

package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/types"
)

// merkle_trees.go exposes a thin facade that x/shield consumes. These tests
// assert the facade delegates verbatim to the member-trust-tree source of
// truth, both for the current root (built and unbuilt cases) and the previous
// root. If the delegation ever drifts, x/shield would silently accept proofs
// against a stale or wrong root, so the parity check is load-bearing.
func TestGetTrustTreeRoot_DelegatesToMemberTree(t *testing.T) {
	f := initFixture(t)
	k := f.keeper
	ctx := f.ctx

	// Before the tree is built both entry points must return exactly the
	// same error. x/shield's validateMerkleRoot depends on this error value.
	gotRoot, gotErr := k.GetTrustTreeRoot(ctx)
	wantRoot, wantErr := k.GetMemberTrustTreeRoot(ctx)
	require.Equal(t, wantRoot, gotRoot)
	require.ErrorIs(t, gotErr, types.ErrTrustTreeNotBuilt)
	require.Equal(t, wantErr, gotErr)
}

func TestGetPreviousTrustTreeRoot_DelegatesToMemberTree(t *testing.T) {
	f := initFixture(t)
	k := f.keeper
	ctx := f.ctx

	got, err := k.GetPreviousTrustTreeRoot(ctx)
	require.NoError(t, err)
	require.Equal(t, k.GetPreviousMemberTrustTreeRoot(ctx), got)
}

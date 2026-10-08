package keeper_test

import (
	"fmt"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/query"
	"github.com/stretchr/testify/require"

	zkcrypto "sparkdream/tools/crypto"
	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

// fetchTrustTreePaged walks the TrustTree query with the given page limit and
// returns the concatenated leaves plus the last response (root, leaf count).
func fetchTrustTreePaged(t *testing.T, qs types.QueryServer, f *fixture, limit uint64) ([]types.TrustTreeLeaf, *types.QueryTrustTreeResponse) {
	t.Helper()
	var (
		leaves []types.TrustTreeLeaf
		last   *types.QueryTrustTreeResponse
		key    []byte
		pages  int
	)
	for {
		resp, err := qs.TrustTree(f.ctx, &types.QueryTrustTreeRequest{
			Pagination: &query.PageRequest{Key: key, Limit: limit},
		})
		require.NoError(t, err)
		require.LessOrEqual(t, uint64(len(resp.Leaves)), limit)
		if last != nil {
			require.Equal(t, last.Root, resp.Root, "root changed between pages")
		}
		leaves = append(leaves, resp.Leaves...)
		last = resp
		pages++
		require.Less(t, pages, 100, "pagination did not terminate")
		if resp.Pagination == nil || len(resp.Pagination.NextKey) == 0 {
			break
		}
		key = resp.Pagination.NextKey
	}
	return leaves, last
}

// sparseRoot rebuilds the tree root from exported (index, hash) leaves, every
// other slot being the zero leaf.
func sparseRoot(t *testing.T, leaves []types.TrustTreeLeaf, depth int) []byte {
	t.Helper()
	m := make(map[uint64][]byte, len(leaves))
	for _, l := range leaves {
		_, dup := m[l.Index]
		require.False(t, dup, "leaf index %d exported twice", l.Index)
		m[l.Index] = l.Hash
	}
	require.NotEmpty(t, m)
	proof, err := zkcrypto.SparseMerkleProof(m, depth, leaves[0].Index)
	require.NoError(t, err)
	return proof.Root
}

// A client paging the export with a small limit gets every leaf exactly once,
// in index order, and rebuilds the on-chain root from them.
func TestTrustTreeQuery_PagedLeavesRebuildRoot(t *testing.T) {
	setTestTreeDepth(t, 4)
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	const n = 5
	for i := 0; i < n; i++ {
		addr := sdk.AccAddress([]byte(fmt.Sprintf("trust_page_member_%02d", i)))
		setupMemberWithZkKey(t, f, addr, types.TrustLevel_TRUST_LEVEL_ESTABLISHED, padKey(fmt.Sprintf("zkpubkey_page_%02d", i)))
	}
	require.NoError(t, f.keeper.MaybeRebuildTrustTree(f.ctx))

	leaves, resp := fetchTrustTreePaged(t, qs, f, 2)
	require.Len(t, leaves, n)
	require.Equal(t, uint64(n), resp.LeafCount)
	for i, leaf := range leaves {
		require.Equal(t, uint64(i), leaf.Index)
	}

	// Dense rebuild (all slots occupied) and sparse rebuild agree with the chain.
	tree := zkcrypto.NewMerkleTree(4)
	for _, leaf := range leaves {
		require.NoError(t, tree.AddLeaf(leaf.Hash))
	}
	require.NoError(t, tree.Build())
	require.Equal(t, resp.Root, tree.Root())
	require.Equal(t, resp.Root, sparseRoot(t, leaves, 4))

	// The unpaged export matches the paged one.
	full, err := qs.TrustTree(f.ctx, &types.QueryTrustTreeRequest{})
	require.NoError(t, err)
	require.Equal(t, leaves, full.Leaves)
}

// Zeroing a member in the middle of the tree deletes its leaf: the export
// skips that index (leaf_count is unchanged, slots are never reused), and a
// client that places leaves by index still rebuilds the new root.
func TestTrustTreeQuery_ZeroedMiddleLeafRebuildsRoot(t *testing.T) {
	setTestTreeDepth(t, 4)
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	const n = 5
	keys := make(map[string]sdk.AccAddress, n) // leaf hash -> member address
	for i := 0; i < n; i++ {
		addr := sdk.AccAddress([]byte(fmt.Sprintf("trust_zero_mid_mem_%02d", i)))
		pk := padKey(fmt.Sprintf("zkpubkey_zmid_%02d", i))
		setupMemberWithZkKey(t, f, addr, types.TrustLevel_TRUST_LEVEL_ESTABLISHED, pk)
		keys[string(zkcrypto.PadTo32(zkcrypto.ComputeLeaf(pk, uint64(types.TrustLevel_TRUST_LEVEL_ESTABLISHED))))] = addr
	}
	require.NoError(t, f.keeper.MaybeRebuildTrustTree(f.ctx))

	before, beforeResp := fetchTrustTreePaged(t, qs, f, 2)
	require.Len(t, before, n)
	require.Equal(t, beforeResp.Root, sparseRoot(t, before, 4))

	// Zero whichever member holds the middle slot.
	const mid = uint64(2)
	require.Equal(t, mid, before[mid].Index)
	midAddr, ok := keys[string(zkcrypto.PadTo32(before[mid].Hash))]
	require.True(t, ok, "middle leaf belongs to no test member")
	member, err := f.keeper.Member.Get(f.ctx, midAddr.String())
	require.NoError(t, err)
	member.Status = types.MemberStatus_MEMBER_STATUS_ZEROED
	require.NoError(t, f.keeper.Member.Set(f.ctx, midAddr.String(), member))
	f.keeper.MarkMemberDirty(f.ctx, midAddr.String())
	require.NoError(t, f.keeper.MaybeRebuildTrustTree(f.ctx))

	after, afterResp := fetchTrustTreePaged(t, qs, f, 2)
	require.NotEqual(t, beforeResp.Root, afterResp.Root)
	require.Equal(t, uint64(n), afterResp.LeafCount, "leaf slots are not reclaimed")
	require.Len(t, after, n-1)
	for _, leaf := range after {
		require.NotEqual(t, mid, leaf.Index, "zeroed leaf still exported")
	}
	require.Equal(t, afterResp.Root, sparseRoot(t, after, 4))

	// Packing the remaining leaves densely (ignoring their indices) gives a
	// different tree: the index is load-bearing.
	dense := zkcrypto.NewMerkleTree(4)
	for _, leaf := range after {
		require.NoError(t, dense.AddLeaf(leaf.Hash))
	}
	require.NoError(t, dense.Build())
	require.NotEqual(t, afterResp.Root, dense.Root())

	// Unpaged fetch agrees.
	full, err := qs.TrustTree(f.ctx, &types.QueryTrustTreeRequest{})
	require.NoError(t, err)
	require.Equal(t, after, full.Leaves)
	require.Equal(t, afterResp.Root, full.Root)
}

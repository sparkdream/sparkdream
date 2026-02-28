package keeper_test

import (
	"context"
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
	zkcrypto "sparkdream/zkprivatevoting/crypto"
)

// setTestTreeDepth overrides the package-level trustTreeDepth for testing
// and reinitializes the zero-hash table. Returns a cleanup function.
func setTestTreeDepth(t *testing.T, depth int) {
	t.Helper()
	keeper.SetTestTreeDepth(depth)
	t.Cleanup(func() { keeper.RestoreTreeDepth() })
}

// ---------------------------------------------------------------------------
// Dirty flag tests
// ---------------------------------------------------------------------------

func TestMarkMemberDirty_And_IsTrustTreeDirty(t *testing.T) {
	t.Run("initially not dirty", func(t *testing.T) {
		f := initFixture(t)
		require.False(t, f.keeper.IsTrustTreeDirty(f.ctx))
	})

	t.Run("dirty after marking a member", func(t *testing.T) {
		f := initFixture(t)
		f.keeper.MarkMemberDirty(f.ctx, "addr1")
		require.True(t, f.keeper.IsTrustTreeDirty(f.ctx))
	})

	t.Run("dirty after full-rebuild flag", func(t *testing.T) {
		f := initFixture(t)
		f.keeper.MarkTrustTreeDirty(f.ctx)
		require.True(t, f.keeper.IsTrustTreeDirty(f.ctx))
	})
}

func TestGetMemberTrustTreeRoot_NotBuilt(t *testing.T) {
	f := initFixture(t)

	root, err := f.keeper.GetMemberTrustTreeRoot(f.ctx)
	require.Error(t, err)
	require.ErrorIs(t, err, types.ErrTrustTreeNotBuilt)
	require.Nil(t, root)
}

func TestGetPreviousMemberTrustTreeRoot_NoPrevious(t *testing.T) {
	f := initFixture(t)

	prevRoot := f.keeper.GetPreviousMemberTrustTreeRoot(f.ctx)
	require.Nil(t, prevRoot)
}

// ---------------------------------------------------------------------------
// Initialization tests
// ---------------------------------------------------------------------------

func TestMaybeRebuildTrustTree_NilVoteKeeper(t *testing.T) {
	f := initFixture(t)
	f.keeper.SetVoteKeeper(nil)

	err := f.keeper.MaybeRebuildTrustTree(f.ctx)
	require.NoError(t, err)
}

func TestMaybeRebuildTrustTree_InitializesOnFirstCall(t *testing.T) {
	setTestTreeDepth(t, 4)
	f := initFixture(t)

	addr := sdk.AccAddress([]byte("trust_init_member___"))
	zkPubKey := []byte("zkpubkey_init_padxxx")
	setupMemberAndVoter(t, f, addr, types.TrustLevel_TRUST_LEVEL_ESTABLISHED, zkPubKey)

	// First call should initialize the tree.
	err := f.keeper.MaybeRebuildTrustTree(f.ctx)
	require.NoError(t, err)

	root, err := f.keeper.GetMemberTrustTreeRoot(f.ctx)
	require.NoError(t, err)
	require.NotNil(t, root)
	require.NotEmpty(t, root)
}

func TestMaybeRebuildTrustTree_NoVoters(t *testing.T) {
	setTestTreeDepth(t, 4)
	f := initFixture(t)

	// Default mock returns no voters — tree initializes but no root set.
	err := f.keeper.MaybeRebuildTrustTree(f.ctx)
	require.NoError(t, err)

	_, err = f.keeper.GetMemberTrustTreeRoot(f.ctx)
	require.ErrorIs(t, err, types.ErrTrustTreeNotBuilt)
}

// ---------------------------------------------------------------------------
// Incremental update tests
// ---------------------------------------------------------------------------

func TestIncrementalUpdate_AddMember(t *testing.T) {
	setTestTreeDepth(t, 4)
	f := initFixture(t)

	addr1 := sdk.AccAddress([]byte("trust_incr_member_1_"))
	zkPubKey1 := []byte("zkpubkey1_incr_padxx")
	setupMemberAndVoter(t, f, addr1, types.TrustLevel_TRUST_LEVEL_ESTABLISHED, zkPubKey1)

	// Initialize.
	err := f.keeper.MaybeRebuildTrustTree(f.ctx)
	require.NoError(t, err)
	root1, err := f.keeper.GetMemberTrustTreeRoot(f.ctx)
	require.NoError(t, err)

	// Add a second member and mark dirty.
	addr2 := sdk.AccAddress([]byte("trust_incr_member_2_"))
	zkPubKey2 := []byte("zkpubkey2_incr_padxx")
	setupMemberAndVoter(t, f, addr2, types.TrustLevel_TRUST_LEVEL_TRUSTED, zkPubKey2)
	f.keeper.MarkMemberDirty(f.ctx, addr2.String())

	err = f.keeper.MaybeRebuildTrustTree(f.ctx)
	require.NoError(t, err)
	root2, err := f.keeper.GetMemberTrustTreeRoot(f.ctx)
	require.NoError(t, err)

	// Root should change after adding a member.
	require.NotEqual(t, root1, root2)
}

func TestIncrementalUpdate_ChangeTrustLevel(t *testing.T) {
	setTestTreeDepth(t, 4)
	f := initFixture(t)

	addr := sdk.AccAddress([]byte("trust_level_change__"))
	zkPubKey := []byte("zkpubkey_level_padxx")
	setupMemberAndVoter(t, f, addr, types.TrustLevel_TRUST_LEVEL_ESTABLISHED, zkPubKey)

	// Initialize.
	err := f.keeper.MaybeRebuildTrustTree(f.ctx)
	require.NoError(t, err)
	root1, err := f.keeper.GetMemberTrustTreeRoot(f.ctx)
	require.NoError(t, err)

	// Change trust level and mark dirty.
	member, err := f.keeper.Member.Get(f.ctx, addr.String())
	require.NoError(t, err)
	member.TrustLevel = types.TrustLevel_TRUST_LEVEL_CORE
	require.NoError(t, f.keeper.Member.Set(f.ctx, addr.String(), member))
	f.keeper.MarkMemberDirty(f.ctx, addr.String())

	err = f.keeper.MaybeRebuildTrustTree(f.ctx)
	require.NoError(t, err)
	root2, err := f.keeper.GetMemberTrustTreeRoot(f.ctx)
	require.NoError(t, err)

	require.NotEqual(t, root1, root2)
}

func TestIncrementalUpdate_ZeroMember(t *testing.T) {
	setTestTreeDepth(t, 4)
	f := initFixture(t)

	addr := sdk.AccAddress([]byte("trust_zero_member___"))
	zkPubKey := []byte("zkpubkey_zero_padxxx")
	setupMemberAndVoter(t, f, addr, types.TrustLevel_TRUST_LEVEL_ESTABLISHED, zkPubKey)

	// Initialize.
	err := f.keeper.MaybeRebuildTrustTree(f.ctx)
	require.NoError(t, err)
	root1, err := f.keeper.GetMemberTrustTreeRoot(f.ctx)
	require.NoError(t, err)

	// Zero the member and mark dirty.
	member, err := f.keeper.Member.Get(f.ctx, addr.String())
	require.NoError(t, err)
	member.Status = types.MemberStatus_MEMBER_STATUS_ZEROED
	require.NoError(t, f.keeper.Member.Set(f.ctx, addr.String(), member))
	f.keeper.MarkMemberDirty(f.ctx, addr.String())

	err = f.keeper.MaybeRebuildTrustTree(f.ctx)
	require.NoError(t, err)
	root2, err := f.keeper.GetMemberTrustTreeRoot(f.ctx)
	require.NoError(t, err)

	require.NotEqual(t, root1, root2)
}

func TestIncrementalUpdate_NonMemberVoterSkipped(t *testing.T) {
	setTestTreeDepth(t, 4)
	f := initFixture(t)

	// VoteKeeper returns an address not in x/rep member list.
	nonMemberAddr := sdk.AccAddress([]byte("not_a_rep_member____"))

	f.voteKeeper.GetActiveVoterZkPublicKeysFn = func(ctx context.Context) ([]string, [][]byte, error) {
		return []string{nonMemberAddr.String()},
			[][]byte{[]byte("zkpubkey_nonmember_x")},
			nil
	}

	err := f.keeper.MaybeRebuildTrustTree(f.ctx)
	require.NoError(t, err)

	_, err = f.keeper.GetMemberTrustTreeRoot(f.ctx)
	require.ErrorIs(t, err, types.ErrTrustTreeNotBuilt)
}

func TestIncrementalUpdate_InactiveMembersExcluded(t *testing.T) {
	setTestTreeDepth(t, 4)
	f := initFixture(t)

	activeAddr := sdk.AccAddress([]byte("trust_active_member_"))
	inactiveAddr := sdk.AccAddress([]byte("trust_inactive_memb_"))
	zkActive := []byte("zkpubkey_active_padx")
	zkInactive := []byte("zkpubkey_inactive_px")

	setupMemberWithStatus(t, f, activeAddr, types.MemberStatus_MEMBER_STATUS_ACTIVE, types.TrustLevel_TRUST_LEVEL_ESTABLISHED)
	setupMemberWithStatus(t, f, inactiveAddr, types.MemberStatus_MEMBER_STATUS_ZEROED, types.TrustLevel_TRUST_LEVEL_NEW)

	f.voteKeeper.GetActiveVoterZkPublicKeysFn = func(ctx context.Context) ([]string, [][]byte, error) {
		return []string{activeAddr.String(), inactiveAddr.String()},
			[][]byte{zkActive, zkInactive},
			nil
	}
	f.voteKeeper.GetVoterZkPublicKeyFn = func(ctx context.Context, address string) ([]byte, error) {
		switch address {
		case activeAddr.String():
			return zkActive, nil
		case inactiveAddr.String():
			return zkInactive, nil
		}
		return nil, types.ErrMemberNotFound
	}

	err := f.keeper.MaybeRebuildTrustTree(f.ctx)
	require.NoError(t, err)

	root, err := f.keeper.GetMemberTrustTreeRoot(f.ctx)
	require.NoError(t, err)
	require.NotNil(t, root)
}

// ---------------------------------------------------------------------------
// Root rotation tests
// ---------------------------------------------------------------------------

func TestRootRotation(t *testing.T) {
	setTestTreeDepth(t, 4)
	f := initFixture(t)

	addr := sdk.AccAddress([]byte("trust_rotate_member_"))
	zkPubKey := []byte("zkpubkey_rotate_padx")
	setupMemberAndVoter(t, f, addr, types.TrustLevel_TRUST_LEVEL_ESTABLISHED, zkPubKey)

	// First build.
	err := f.keeper.MaybeRebuildTrustTree(f.ctx)
	require.NoError(t, err)
	firstRoot, err := f.keeper.GetMemberTrustTreeRoot(f.ctx)
	require.NoError(t, err)

	// No previous root after first build.
	prevRoot := f.keeper.GetPreviousMemberTrustTreeRoot(f.ctx)
	require.Nil(t, prevRoot)

	// Change trust level to get a different root.
	member, err := f.keeper.Member.Get(f.ctx, addr.String())
	require.NoError(t, err)
	member.TrustLevel = types.TrustLevel_TRUST_LEVEL_CORE
	require.NoError(t, f.keeper.Member.Set(f.ctx, addr.String(), member))
	f.keeper.MarkMemberDirty(f.ctx, addr.String())

	err = f.keeper.MaybeRebuildTrustTree(f.ctx)
	require.NoError(t, err)

	secondRoot, err := f.keeper.GetMemberTrustTreeRoot(f.ctx)
	require.NoError(t, err)

	// Previous root should be the first root.
	prevRoot = f.keeper.GetPreviousMemberTrustTreeRoot(f.ctx)
	require.NotNil(t, prevRoot)
	require.Equal(t, firstRoot, prevRoot)
	require.NotEqual(t, firstRoot, secondRoot)
}

// ---------------------------------------------------------------------------
// Full rebuild test
// ---------------------------------------------------------------------------

func TestFullRebuild_ViaMarkTrustTreeDirty(t *testing.T) {
	setTestTreeDepth(t, 4)
	f := initFixture(t)

	addr := sdk.AccAddress([]byte("trust_fullrebuild___"))
	zkPubKey := []byte("zkpubkey_fullreb_pad")
	setupMemberAndVoter(t, f, addr, types.TrustLevel_TRUST_LEVEL_ESTABLISHED, zkPubKey)

	// Initialize.
	err := f.keeper.MaybeRebuildTrustTree(f.ctx)
	require.NoError(t, err)
	root1, err := f.keeper.GetMemberTrustTreeRoot(f.ctx)
	require.NoError(t, err)

	// Request full rebuild.
	f.keeper.MarkTrustTreeDirty(f.ctx)
	err = f.keeper.MaybeRebuildTrustTree(f.ctx)
	require.NoError(t, err)

	root2, err := f.keeper.GetMemberTrustTreeRoot(f.ctx)
	require.NoError(t, err)

	// Root should be identical since same members.
	require.Equal(t, root1, root2)
}

func TestRebuildMemberTrustTree_NoVoteKeeper(t *testing.T) {
	f := initFixture(t)
	f.keeper.SetVoteKeeper(nil)

	err := f.keeper.RebuildMemberTrustTree(f.ctx)
	require.NoError(t, err)
}

// ---------------------------------------------------------------------------
// Root consistency: KV incremental tree must match zkcrypto.MerkleTree
// ---------------------------------------------------------------------------

func TestRootConsistency_MatchesZkcryptoMerkleTree(t *testing.T) {
	depth := 4
	setTestTreeDepth(t, depth)
	f := initFixture(t)

	// Create 3 members with known ZK keys and trust levels.
	members := []struct {
		addr     sdk.AccAddress
		zkPubKey []byte
		trust    types.TrustLevel
	}{
		{sdk.AccAddress([]byte("consistency_member_1")), []byte("zkpubkey_consist_1__"), types.TrustLevel_TRUST_LEVEL_ESTABLISHED},
		{sdk.AccAddress([]byte("consistency_member_2")), []byte("zkpubkey_consist_2__"), types.TrustLevel_TRUST_LEVEL_TRUSTED},
		{sdk.AccAddress([]byte("consistency_member_3")), []byte("zkpubkey_consist_3__"), types.TrustLevel_TRUST_LEVEL_CORE},
	}

	for _, m := range members {
		setupMemberAndVoter(t, f, m.addr, m.trust, m.zkPubKey)
	}

	// Build with incremental KV tree.
	err := f.keeper.MaybeRebuildTrustTree(f.ctx)
	require.NoError(t, err)

	kvRoot, err := f.keeper.GetMemberTrustTreeRoot(f.ctx)
	require.NoError(t, err)

	// Build the same tree with zkcrypto.MerkleTree.
	refTree := zkcrypto.NewMerkleTree(depth)
	for _, m := range members {
		leaf := zkcrypto.ComputeLeaf(m.zkPubKey, uint64(m.trust))
		require.NoError(t, refTree.AddLeaf(leaf))
	}
	require.NoError(t, refTree.Build())

	require.Equal(t, refTree.Root(), kvRoot, "incremental KV tree root must match zkcrypto.MerkleTree root")

	// Verify a proof from the reference tree.
	proof, err := refTree.GetProof(0)
	require.NoError(t, err)
	require.True(t, proof.Verify(), "proof generated from reference tree should verify against shared root")
}

// ---------------------------------------------------------------------------
// MaybeRebuildTrustTree no-op when nothing dirty
// ---------------------------------------------------------------------------

func TestMaybeRebuildTrustTree_NoopWhenClean(t *testing.T) {
	setTestTreeDepth(t, 4)
	f := initFixture(t)

	addr := sdk.AccAddress([]byte("trust_noop_member___"))
	zkPubKey := []byte("zkpubkey_noop_padxxx")
	setupMemberAndVoter(t, f, addr, types.TrustLevel_TRUST_LEVEL_ESTABLISHED, zkPubKey)

	// Initialize.
	err := f.keeper.MaybeRebuildTrustTree(f.ctx)
	require.NoError(t, err)
	root1, err := f.keeper.GetMemberTrustTreeRoot(f.ctx)
	require.NoError(t, err)

	// Second call with no dirty members should be a no-op.
	err = f.keeper.MaybeRebuildTrustTree(f.ctx)
	require.NoError(t, err)
	root2, err := f.keeper.GetMemberTrustTreeRoot(f.ctx)
	require.NoError(t, err)

	require.Equal(t, root1, root2)

	// Previous root should still be nil (no rotation on no-op).
	require.Nil(t, f.keeper.GetPreviousMemberTrustTreeRoot(f.ctx))
}

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

func setupMemberAndVoter(t *testing.T, f *fixture, addr sdk.AccAddress, trust types.TrustLevel, zkPubKey []byte) {
	t.Helper()
	setupMemberWithStatus(t, f, addr, types.MemberStatus_MEMBER_STATUS_ACTIVE, trust)

	// Wire up the mock VoteKeeper to return this member's ZK key.
	oldGetAll := f.voteKeeper.GetActiveVoterZkPublicKeysFn
	oldGetOne := f.voteKeeper.GetVoterZkPublicKeyFn

	f.voteKeeper.GetActiveVoterZkPublicKeysFn = func(ctx context.Context) ([]string, [][]byte, error) {
		addrs := []string{addr.String()}
		keys := [][]byte{zkPubKey}
		if oldGetAll != nil {
			prevAddrs, prevKeys, err := oldGetAll(ctx)
			if err != nil {
				return nil, nil, err
			}
			addrs = append(prevAddrs, addrs...)
			keys = append(prevKeys, keys...)
		}
		return addrs, keys, nil
	}

	f.voteKeeper.GetVoterZkPublicKeyFn = func(ctx context.Context, address string) ([]byte, error) {
		if address == addr.String() {
			return zkPubKey, nil
		}
		if oldGetOne != nil {
			return oldGetOne(ctx, address)
		}
		return nil, types.ErrMemberNotFound
	}
}

func setupMemberWithStatus(t *testing.T, f *fixture, addr sdk.AccAddress, status types.MemberStatus, trust types.TrustLevel) {
	t.Helper()
	member := types.Member{
		Address:          addr.String(),
		DreamBalance:     PtrInt(math.ZeroInt()),
		StakedDream:      PtrInt(math.ZeroInt()),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		Status:           status,
		TrustLevel:       trust,
		ReputationScores: map[string]string{},
	}
	require.NoError(t, f.keeper.Member.Set(f.ctx, addr.String(), member))
}

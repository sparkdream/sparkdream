package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	zkcrypto "sparkdream/tools/crypto"
	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
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

func TestMaybeRebuildTrustTree_NoMembers(t *testing.T) {
	setTestTreeDepth(t, 4)
	f := initFixture(t)

	// No members with ZK keys — tree should not be built.
	err := f.keeper.MaybeRebuildTrustTree(f.ctx)
	require.NoError(t, err)

	_, err = f.keeper.GetMemberTrustTreeRoot(f.ctx)
	require.ErrorIs(t, err, types.ErrTrustTreeNotBuilt)
}

func TestMaybeRebuildTrustTree_InitializesWithZkKey(t *testing.T) {
	setTestTreeDepth(t, 4)
	f := initFixture(t)

	addr := sdk.AccAddress([]byte("trust_init_member___"))
	zkPubKey := make([]byte, 32)
	copy(zkPubKey, []byte("zkpubkey_init_padxxx"))
	setupMemberWithZkKey(t, f, addr, types.TrustLevel_TRUST_LEVEL_ESTABLISHED, zkPubKey)

	err := f.keeper.MaybeRebuildTrustTree(f.ctx)
	require.NoError(t, err)

	root, err := f.keeper.GetMemberTrustTreeRoot(f.ctx)
	require.NoError(t, err)
	require.NotNil(t, root)
	require.NotEmpty(t, root)
}

func TestMaybeRebuildTrustTree_SkipsMembersWithoutZkKey(t *testing.T) {
	setTestTreeDepth(t, 4)
	f := initFixture(t)

	// Member without ZK key — should be skipped.
	addr := sdk.AccAddress([]byte("trust_no_zkkey______"))
	setupMemberWithStatus(t, f, addr, types.MemberStatus_MEMBER_STATUS_ACTIVE, types.TrustLevel_TRUST_LEVEL_ESTABLISHED)

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
	zkPubKey1 := make([]byte, 32)
	copy(zkPubKey1, []byte("zkpubkey1_incr_padxx"))
	setupMemberWithZkKey(t, f, addr1, types.TrustLevel_TRUST_LEVEL_ESTABLISHED, zkPubKey1)

	err := f.keeper.MaybeRebuildTrustTree(f.ctx)
	require.NoError(t, err)
	root1, err := f.keeper.GetMemberTrustTreeRoot(f.ctx)
	require.NoError(t, err)

	// Add second member and mark dirty.
	addr2 := sdk.AccAddress([]byte("trust_incr_member_2_"))
	zkPubKey2 := make([]byte, 32)
	copy(zkPubKey2, []byte("zkpubkey2_incr_padxx"))
	setupMemberWithZkKey(t, f, addr2, types.TrustLevel_TRUST_LEVEL_TRUSTED, zkPubKey2)
	f.keeper.MarkMemberDirty(f.ctx, addr2.String())

	err = f.keeper.MaybeRebuildTrustTree(f.ctx)
	require.NoError(t, err)
	root2, err := f.keeper.GetMemberTrustTreeRoot(f.ctx)
	require.NoError(t, err)

	require.NotEqual(t, root1, root2)
}

func TestIncrementalUpdate_ChangeTrustLevel(t *testing.T) {
	setTestTreeDepth(t, 4)
	f := initFixture(t)

	addr := sdk.AccAddress([]byte("trust_level_change__"))
	zkPubKey := make([]byte, 32)
	copy(zkPubKey, []byte("zkpubkey_level_padxx"))
	setupMemberWithZkKey(t, f, addr, types.TrustLevel_TRUST_LEVEL_ESTABLISHED, zkPubKey)

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
	zkPubKey := make([]byte, 32)
	copy(zkPubKey, []byte("zkpubkey_zero_padxxx"))
	setupMemberWithZkKey(t, f, addr, types.TrustLevel_TRUST_LEVEL_ESTABLISHED, zkPubKey)

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

// ---------------------------------------------------------------------------
// Root rotation tests
// ---------------------------------------------------------------------------

func TestRootRotation(t *testing.T) {
	setTestTreeDepth(t, 4)
	f := initFixture(t)

	addr := sdk.AccAddress([]byte("trust_rotate_member_"))
	zkPubKey := make([]byte, 32)
	copy(zkPubKey, []byte("zkpubkey_rotate_padx"))
	setupMemberWithZkKey(t, f, addr, types.TrustLevel_TRUST_LEVEL_ESTABLISHED, zkPubKey)

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
	zkPubKey := make([]byte, 32)
	copy(zkPubKey, []byte("zkpubkey_fullreb_pad"))
	setupMemberWithZkKey(t, f, addr, types.TrustLevel_TRUST_LEVEL_ESTABLISHED, zkPubKey)

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

func TestRebuildMemberTrustTree_ForceRebuild(t *testing.T) {
	setTestTreeDepth(t, 4)
	f := initFixture(t)

	addr := sdk.AccAddress([]byte("trust_force_rebuild_"))
	zkPubKey := make([]byte, 32)
	copy(zkPubKey, []byte("zkpubkey_force_padxx"))
	setupMemberWithZkKey(t, f, addr, types.TrustLevel_TRUST_LEVEL_ESTABLISHED, zkPubKey)

	err := f.keeper.RebuildMemberTrustTree(f.ctx)
	require.NoError(t, err)

	root, err := f.keeper.GetMemberTrustTreeRoot(f.ctx)
	require.NoError(t, err)
	require.NotNil(t, root)
}

// ---------------------------------------------------------------------------
// Root consistency: KV incremental tree must match zkcrypto.MerkleTree
// ---------------------------------------------------------------------------

func TestRootConsistency_MatchesZkcryptoMerkleTree(t *testing.T) {
	depth := 4
	setTestTreeDepth(t, depth)
	f := initFixture(t)

	members := []struct {
		addr     sdk.AccAddress
		zkPubKey []byte
		trust    types.TrustLevel
	}{
		{sdk.AccAddress([]byte("consistency_member_1")), padKey("zkpubkey_consist_1__"), types.TrustLevel_TRUST_LEVEL_ESTABLISHED},
		{sdk.AccAddress([]byte("consistency_member_2")), padKey("zkpubkey_consist_2__"), types.TrustLevel_TRUST_LEVEL_TRUSTED},
		{sdk.AccAddress([]byte("consistency_member_3")), padKey("zkpubkey_consist_3__"), types.TrustLevel_TRUST_LEVEL_CORE},
	}

	for _, m := range members {
		setupMemberWithZkKey(t, f, m.addr, m.trust, m.zkPubKey)
	}

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
}

// ---------------------------------------------------------------------------
// No-op when clean
// ---------------------------------------------------------------------------

func TestMaybeRebuildTrustTree_NoopWhenClean(t *testing.T) {
	setTestTreeDepth(t, 4)
	f := initFixture(t)

	addr := sdk.AccAddress([]byte("trust_noop_member___"))
	zkPubKey := make([]byte, 32)
	copy(zkPubKey, []byte("zkpubkey_noop_padxxx"))
	setupMemberWithZkKey(t, f, addr, types.TrustLevel_TRUST_LEVEL_ESTABLISHED, zkPubKey)

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
	require.Nil(t, f.keeper.GetPreviousMemberTrustTreeRoot(f.ctx))
}

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// padKey pads a string to 32 bytes for use as a ZK public key.
func padKey(s string) []byte {
	key := make([]byte, 32)
	copy(key, []byte(s))
	return key
}

func setupMemberWithZkKey(t *testing.T, f *fixture, addr sdk.AccAddress, trust types.TrustLevel, zkPubKey []byte) {
	t.Helper()
	member := types.Member{
		Address:          addr.String(),
		DreamBalance:     PtrInt(math.ZeroInt()),
		StakedDream:      PtrInt(math.ZeroInt()),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		Status:           types.MemberStatus_MEMBER_STATUS_ACTIVE,
		TrustLevel:       trust,
		ReputationScores: map[string]string{},
		ZkPublicKey:      zkPubKey,
	}
	require.NoError(t, f.keeper.Member.Set(f.ctx, addr.String(), member))
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

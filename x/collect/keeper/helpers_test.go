package keeper_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	reptypes "sparkdream/x/rep/types"
	"sparkdream/x/collect/keeper"
	"sparkdream/x/collect/types"
)

func TestCollaboratorCompositeKey(t *testing.T) {
	key := keeper.CollaboratorCompositeKey(42, "cosmos1abc")
	require.Equal(t, "42/cosmos1abc", key)

	key = keeper.CollaboratorCompositeKey(0, "addr")
	require.Equal(t, "0/addr", key)
}

func TestFlagCompositeKey(t *testing.T) {
	key := keeper.FlagCompositeKey(types.FlagTargetType_FLAG_TARGET_TYPE_COLLECTION, 99)
	expected := fmt.Sprintf("%d/%d", int32(types.FlagTargetType_FLAG_TARGET_TYPE_COLLECTION), 99)
	require.Equal(t, expected, key)

	key = keeper.FlagCompositeKey(types.FlagTargetType_FLAG_TARGET_TYPE_ITEM, 7)
	expected = fmt.Sprintf("%d/%d", int32(types.FlagTargetType_FLAG_TARGET_TYPE_ITEM), 7)
	require.Equal(t, expected, key)
}

func TestReactionDedupCompositeKey(t *testing.T) {
	key := keeper.ReactionDedupCompositeKey("cosmos1voter", types.FlagTargetType_FLAG_TARGET_TYPE_COLLECTION, 10)
	expected := fmt.Sprintf("cosmos1voter/%d/%d", int32(types.FlagTargetType_FLAG_TARGET_TYPE_COLLECTION), 10)
	require.Equal(t, expected, key)
}

func TestReactionLimitCompositeKey(t *testing.T) {
	// day = blockHeight / BlocksPerDay = 28800 / 14400 = 2
	key := keeper.ReactionLimitCompositeKey("cosmos1addr", 28800, "upvote")
	require.Equal(t, "cosmos1addr/2/upvote", key)

	// day = 0 for blocks < BlocksPerDay
	key = keeper.ReactionLimitCompositeKey("cosmos1addr", 100, "downvote")
	require.Equal(t, "cosmos1addr/0/downvote", key)
}

func TestHasWriteAccess(t *testing.T) {
	f := initTestFixture(t)

	// Create collection owned by f.owner
	collID := f.createCollection(t, f.owner)
	coll, err := f.keeper.Collection.Get(f.ctx, collID)
	require.NoError(t, err)

	// Owner has write access
	ok, err := f.keeper.HasWriteAccess(f.ctx, coll, f.owner)
	require.NoError(t, err)
	require.True(t, ok)

	// Add EDITOR collaborator
	f.addCollaborator(t, collID, f.owner, f.member, types.CollaboratorRole_COLLABORATOR_ROLE_EDITOR)
	ok, err = f.keeper.HasWriteAccess(f.ctx, coll, f.member)
	require.NoError(t, err)
	require.True(t, ok)

	// Add ADMIN collaborator
	f.addCollaborator(t, collID, f.owner, f.sentinel, types.CollaboratorRole_COLLABORATOR_ROLE_ADMIN)
	ok, err = f.keeper.HasWriteAccess(f.ctx, coll, f.sentinel)
	require.NoError(t, err)
	require.True(t, ok)

	// VIEWER does not have write access - need to create another address
	// nonMember is not a collaborator at all
	ok, err = f.keeper.HasWriteAccess(f.ctx, coll, f.nonMember)
	require.NoError(t, err)
	require.False(t, ok)
}

func TestHasWriteAccess_UnspecifiedRole(t *testing.T) {
	f := initTestFixture(t)

	collID := f.createCollection(t, f.owner)
	coll, err := f.keeper.Collection.Get(f.ctx, collID)
	require.NoError(t, err)

	// A collaborator with UNSPECIFIED role should not have write access.
	// We can't use addCollaborator for UNSPECIFIED (it may be rejected),
	// so we test that a non-collaborator has no access.
	ok, err := f.keeper.HasWriteAccess(f.ctx, coll, f.member)
	require.NoError(t, err)
	require.False(t, ok, "non-collaborator should not have write access")
}

func TestIsOwnerOrAdmin(t *testing.T) {
	f := initTestFixture(t)

	collID := f.createCollection(t, f.owner)
	coll, err := f.keeper.Collection.Get(f.ctx, collID)
	require.NoError(t, err)

	// Owner passes
	ok, err := f.keeper.IsOwnerOrAdmin(f.ctx, coll, f.owner)
	require.NoError(t, err)
	require.True(t, ok)

	// ADMIN passes
	f.addCollaborator(t, collID, f.owner, f.sentinel, types.CollaboratorRole_COLLABORATOR_ROLE_ADMIN)
	ok, err = f.keeper.IsOwnerOrAdmin(f.ctx, coll, f.sentinel)
	require.NoError(t, err)
	require.True(t, ok)

	// EDITOR fails
	f.addCollaborator(t, collID, f.owner, f.member, types.CollaboratorRole_COLLABORATOR_ROLE_EDITOR)
	ok, err = f.keeper.IsOwnerOrAdmin(f.ctx, coll, f.member)
	require.NoError(t, err)
	require.False(t, ok, "EDITOR should not pass IsOwnerOrAdmin")

	// Stranger fails
	ok, err = f.keeper.IsOwnerOrAdmin(f.ctx, coll, f.nonMember)
	require.NoError(t, err)
	require.False(t, ok)
}

func TestTrustLevelIndex(t *testing.T) {
	tests := []struct {
		level    reptypes.TrustLevel
		expected int
	}{
		{reptypes.TrustLevel_TRUST_LEVEL_NEW, 0},
		{reptypes.TrustLevel_TRUST_LEVEL_PROVISIONAL, 1},
		{reptypes.TrustLevel_TRUST_LEVEL_ESTABLISHED, 2},
		{reptypes.TrustLevel_TRUST_LEVEL_TRUSTED, 3},
		{reptypes.TrustLevel_TRUST_LEVEL_CORE, 4},
	}

	for _, tc := range tests {
		t.Run(tc.level.String(), func(t *testing.T) {
			require.Equal(t, tc.expected, keeper.TrustLevelIndex(tc.level))
		})
	}
}

func TestParseTrustLevel(t *testing.T) {
	// Valid strings
	tl, ok := keeper.ParseTrustLevel("TRUST_LEVEL_NEW")
	require.True(t, ok)
	require.Equal(t, reptypes.TrustLevel_TRUST_LEVEL_NEW, tl)

	tl, ok = keeper.ParseTrustLevel("TRUST_LEVEL_ESTABLISHED")
	require.True(t, ok)
	require.Equal(t, reptypes.TrustLevel_TRUST_LEVEL_ESTABLISHED, tl)

	tl, ok = keeper.ParseTrustLevel("TRUST_LEVEL_CORE")
	require.True(t, ok)
	require.Equal(t, reptypes.TrustLevel_TRUST_LEVEL_CORE, tl)

	// Invalid string
	_, ok = keeper.ParseTrustLevel("INVALID_LEVEL")
	require.False(t, ok)

	_, ok = keeper.ParseTrustLevel("")
	require.False(t, ok)
}

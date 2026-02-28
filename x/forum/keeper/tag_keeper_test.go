package keeper_test

import (
	"testing"

	"cosmossdk.io/collections"
	"github.com/stretchr/testify/require"

	commontypes "sparkdream/x/common/types"
)

// --- TagExists ---

func TestTagExists_Exists(t *testing.T) {
	f := initFixture(t)

	f.createTestTag(t, "cosmos")

	exists, err := f.keeper.TagExists(f.ctx, "cosmos")
	require.NoError(t, err)
	require.True(t, exists)
}

func TestTagExists_DoesNotExist(t *testing.T) {
	f := initFixture(t)

	exists, err := f.keeper.TagExists(f.ctx, "nonexistent")
	require.NoError(t, err)
	require.False(t, exists)
}

// --- IsReservedTag ---

func TestIsReservedTag_Reserved(t *testing.T) {
	f := initFixture(t)

	// Manually set a reserved tag
	rt := commontypes.ReservedTag{
		Name:      "governance",
		Authority: testAuthority,
	}
	err := f.keeper.ReservedTag.Set(f.ctx, "governance", rt)
	require.NoError(t, err)

	reserved, err := f.keeper.IsReservedTag(f.ctx, "governance")
	require.NoError(t, err)
	require.True(t, reserved)
}

func TestIsReservedTag_NotReserved(t *testing.T) {
	f := initFixture(t)

	reserved, err := f.keeper.IsReservedTag(f.ctx, "general")
	require.NoError(t, err)
	require.False(t, reserved)
}

// --- GetTag ---

func TestGetTag_Success(t *testing.T) {
	f := initFixture(t)

	tag := f.createTestTag(t, "defi")

	got, err := f.keeper.GetTag(f.ctx, "defi")
	require.NoError(t, err)
	require.Equal(t, tag.Name, got.Name)
	require.Equal(t, tag.CreatedAt, got.CreatedAt)
	require.Equal(t, uint64(0), got.UsageCount)
}

func TestGetTag_NotFound(t *testing.T) {
	f := initFixture(t)

	_, err := f.keeper.GetTag(f.ctx, "missing")
	require.Error(t, err)
	require.ErrorIs(t, err, collections.ErrNotFound)
}

// --- IncrementTagUsage ---

func TestIncrementTagUsage_Success(t *testing.T) {
	f := initFixture(t)

	f.createTestTag(t, "staking")

	// Increment once
	err := f.keeper.IncrementTagUsage(f.ctx, "staking", 1000)
	require.NoError(t, err)

	tag, err := f.keeper.GetTag(f.ctx, "staking")
	require.NoError(t, err)
	require.Equal(t, uint64(1), tag.UsageCount)
	require.Equal(t, int64(1000), tag.LastUsedAt)

	// Increment again with a later timestamp
	err = f.keeper.IncrementTagUsage(f.ctx, "staking", 2000)
	require.NoError(t, err)

	tag, err = f.keeper.GetTag(f.ctx, "staking")
	require.NoError(t, err)
	require.Equal(t, uint64(2), tag.UsageCount)
	require.Equal(t, int64(2000), tag.LastUsedAt)
}

func TestIncrementTagUsage_NonExistentTag(t *testing.T) {
	f := initFixture(t)

	err := f.keeper.IncrementTagUsage(f.ctx, "nonexistent", 1000)
	require.Error(t, err)
	require.ErrorIs(t, err, collections.ErrNotFound)
}

// --- Table-driven comprehensive test ---

func TestTagKeeper_InterfaceCompliance(t *testing.T) {
	// This test verifies the TagKeeper interface contract end-to-end
	f := initFixture(t)

	tests := []struct {
		name string
		run  func(t *testing.T)
	}{
		{
			name: "create tag and verify exists",
			run: func(t *testing.T) {
				f.createTestTag(t, "alpha")
				exists, err := f.keeper.TagExists(f.ctx, "alpha")
				require.NoError(t, err)
				require.True(t, exists)
			},
		},
		{
			name: "reserved tag check does not affect regular tags",
			run: func(t *testing.T) {
				f.createTestTag(t, "beta")
				reserved, err := f.keeper.IsReservedTag(f.ctx, "beta")
				require.NoError(t, err)
				require.False(t, reserved, "regular tag should not be reserved")
			},
		},
		{
			name: "increment preserves other tag fields",
			run: func(t *testing.T) {
				tag := f.createTestTag(t, "gamma")
				err := f.keeper.IncrementTagUsage(f.ctx, "gamma", 5000)
				require.NoError(t, err)

				got, err := f.keeper.GetTag(f.ctx, "gamma")
				require.NoError(t, err)
				require.Equal(t, tag.Name, got.Name)
				require.Equal(t, tag.CreatedAt, got.CreatedAt)
				require.Equal(t, uint64(1), got.UsageCount)
				require.Equal(t, int64(5000), got.LastUsedAt)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, tc.run)
	}
}

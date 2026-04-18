package keeper_test

import (
	"testing"

	"cosmossdk.io/collections"
	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/types"
)

func TestTag_SetExistsGetRemove(t *testing.T) {
	f := initFixture(t)
	k := f.keeper
	ctx := f.ctx

	// initFixture seeds some tags; pick a fresh one that is not seeded.
	const name = "salvation_only_tag"

	exists, err := k.TagExists(ctx, name)
	require.NoError(t, err)
	require.False(t, exists)

	require.NoError(t, k.SetTag(ctx, types.Tag{Name: name, UsageCount: 3, LastUsedAt: 100}))

	exists, err = k.TagExists(ctx, name)
	require.NoError(t, err)
	require.True(t, exists)

	got, err := k.GetTag(ctx, name)
	require.NoError(t, err)
	require.Equal(t, name, got.Name)
	require.Equal(t, uint64(3), got.UsageCount)
	require.Equal(t, int64(100), got.LastUsedAt)

	require.NoError(t, k.RemoveTag(ctx, name))
	exists, err = k.TagExists(ctx, name)
	require.NoError(t, err)
	require.False(t, exists)

	_, err = k.GetTag(ctx, name)
	require.ErrorIs(t, err, collections.ErrNotFound)
}

func TestTag_IncrementUsage(t *testing.T) {
	f := initFixture(t)
	k := f.keeper
	ctx := f.ctx

	const name = "countme"
	require.NoError(t, k.SetTag(ctx, types.Tag{Name: name}))

	require.NoError(t, k.IncrementTagUsage(ctx, name, 10))
	require.NoError(t, k.IncrementTagUsage(ctx, name, 20))

	got, err := k.GetTag(ctx, name)
	require.NoError(t, err)
	require.Equal(t, uint64(2), got.UsageCount)
	require.Equal(t, int64(20), got.LastUsedAt, "last_used_at follows the most recent call")
}

func TestTag_IncrementUsage_UnknownTag(t *testing.T) {
	f := initFixture(t)
	k := f.keeper

	err := k.IncrementTagUsage(f.ctx, "never_registered", 1)
	require.ErrorIs(t, err, collections.ErrNotFound)
}

func TestReservedTag_SetIsReservedGetRemove(t *testing.T) {
	f := initFixture(t)
	k := f.keeper
	ctx := f.ctx

	const name = "founders_only"

	reserved, err := k.IsReservedTag(ctx, name)
	require.NoError(t, err)
	require.False(t, reserved)

	require.NoError(t, k.SetReservedTag(ctx, types.ReservedTag{Name: name, Authority: "gov", MembersCanUse: true}))

	reserved, err = k.IsReservedTag(ctx, name)
	require.NoError(t, err)
	require.True(t, reserved)

	got, err := k.GetReservedTag(ctx, name)
	require.NoError(t, err)
	require.Equal(t, name, got.Name)
	require.Equal(t, "gov", got.Authority)
	require.True(t, got.MembersCanUse)

	require.NoError(t, k.RemoveReservedTag(ctx, name))
	reserved, err = k.IsReservedTag(ctx, name)
	require.NoError(t, err)
	require.False(t, reserved)
}

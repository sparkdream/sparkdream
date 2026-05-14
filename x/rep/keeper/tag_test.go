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

// Rolling-expiry contract: a tag that is actively referenced must roll its
// LastUsedAt (and therefore its effective GC deadline of
// last_used_at + DefaultTagExpiration) forward on every IncrementTagUsage
// call. Without this, long-lived tags would expire on their original
// schedule regardless of usage and ExpireTags would silently wipe popular
// tags + their references.
func TestTag_IncrementUsage_RollsExpiration(t *testing.T) {
	f := initFixture(t)
	k := f.keeper
	ctx := f.ctx

	const name = "rolling"
	// Seed with a stale last-used — exactly the situation the increment must
	// rescue the tag from before ExpireTags can reach it.
	require.NoError(t, k.SetTag(ctx, types.Tag{
		Name: name, CreatedAt: 0, LastUsedAt: 1,
	}))

	// First use at t=1000 — must push LastUsedAt past the stale value.
	require.NoError(t, k.IncrementTagUsage(ctx, name, 1000))
	got, err := k.GetTag(ctx, name)
	require.NoError(t, err)
	require.Equal(t, int64(1000), got.LastUsedAt)

	// A later use at t=2000 must push LastUsedAt further out — confirming
	// the effective deadline rolls forward to t + DefaultTagExpiration.
	require.NoError(t, k.IncrementTagUsage(ctx, name, 2000))
	got, err = k.GetTag(ctx, name)
	require.NoError(t, err)
	require.Equal(t, int64(2000), got.LastUsedAt,
		"each use must roll last_used_at forward — that is the GC deadline source")

	// Sanity: at now = 2000 + DefaultTagExpiration - 1, the tag must survive
	// GC; at now = 2000 + DefaultTagExpiration, it must be reclaimed.
	require.NoError(t, k.ExpireTags(ctx, 2000+types.DefaultTagExpiration-1))
	exists, err := k.TagExists(ctx, name)
	require.NoError(t, err)
	require.True(t, exists, "tag must survive GC one second before its rolled deadline")

	require.NoError(t, k.ExpireTags(ctx, 2000+types.DefaultTagExpiration))
	exists, err = k.TagExists(ctx, name)
	require.NoError(t, err)
	require.False(t, exists, "tag must be reclaimed once it hits its rolled deadline")
}

func TestTag_IncrementUsage_UnknownTag(t *testing.T) {
	f := initFixture(t)
	k := f.keeper

	err := k.IncrementTagUsage(f.ctx, "never_registered", 1)
	require.ErrorIs(t, err, collections.ErrNotFound)
}

func TestTag_DecrementUsage(t *testing.T) {
	f := initFixture(t)
	k := f.keeper
	ctx := f.ctx

	const name = "dropme"
	// Seed at usage_count=3 with a fixed last_used_at so we can prove
	// DecrementTagUsage leaves LastUsedAt — and therefore the effective GC
	// deadline (last_used_at + DefaultTagExpiration) — untouched.
	require.NoError(t, k.SetTag(ctx, types.Tag{
		Name: name, UsageCount: 3, LastUsedAt: 100,
	}))

	require.NoError(t, k.DecrementTagUsage(ctx, name))
	require.NoError(t, k.DecrementTagUsage(ctx, name))

	got, err := k.GetTag(ctx, name)
	require.NoError(t, err)
	require.Equal(t, uint64(1), got.UsageCount)
	require.Equal(t, int64(100), got.LastUsedAt,
		"decrement must not touch LastUsedAt — the GC schedule must not be pulled in by a release")
}

func TestTag_DecrementUsage_FloorAtZero(t *testing.T) {
	f := initFixture(t)
	k := f.keeper
	ctx := f.ctx

	const name = "underflow_me"
	require.NoError(t, k.SetTag(ctx, types.Tag{Name: name, UsageCount: 0}))

	// Decrementing a tag already at 0 must clamp rather than underflow into
	// a giant uint64 — the caller's transaction shouldn't fail just because
	// some upstream drift already over-decremented.
	require.NoError(t, k.DecrementTagUsage(ctx, name))

	got, err := k.GetTag(ctx, name)
	require.NoError(t, err)
	require.Equal(t, uint64(0), got.UsageCount)
}

func TestTag_DecrementUsage_UnknownTag(t *testing.T) {
	f := initFixture(t)
	k := f.keeper

	// ErrNotFound surfaces back to the caller — content modules log and
	// continue (the tag was GC'd between create and edit), they don't abort.
	err := k.DecrementTagUsage(f.ctx, "never_registered")
	require.ErrorIs(t, err, collections.ErrNotFound)
}

func TestTag_IncrementThenDecrement_RoundTrip(t *testing.T) {
	f := initFixture(t)
	k := f.keeper
	ctx := f.ctx

	const name = "roundtrip"
	require.NoError(t, k.SetTag(ctx, types.Tag{Name: name}))

	require.NoError(t, k.IncrementTagUsage(ctx, name, 10))
	require.NoError(t, k.IncrementTagUsage(ctx, name, 20))

	require.NoError(t, k.DecrementTagUsage(ctx, name))

	got, err := k.GetTag(ctx, name)
	require.NoError(t, err)
	require.Equal(t, uint64(1), got.UsageCount)
	require.Equal(t, int64(20), got.LastUsedAt,
		"decrement after increment leaves LastUsedAt at the last increment — the GC deadline still anchors to the last real use, not the release")
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

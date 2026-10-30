package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/forum/types"
	reptypes "sparkdream/x/rep/types"
)

// FRM-19: removing a tag must not walk every post in the removing tx. The
// tag is queued and the EndBlocker strips it in capped batches; a tag that
// is re-created before the walk ends is dropped from the queue untouched.
func TestPruneTagReferencesQueuedAndBatched(t *testing.T) {
	f := initFixture(t)

	const n = 450 // > 2 batches of maxTagPruneScanPerBlock (200)
	for i := uint64(1); i <= n; i++ {
		tags := []string{"aurora"}
		if i%2 == 0 {
			tags = []string{"zenith", "phoenix"}
		}
		require.NoError(t, f.keeper.Post.Set(f.ctx, i, types.Post{PostId: i, Tags: tags}))
	}
	delete(f.repKeeper.tags, "aurora")
	delete(f.repKeeper.tags, "zenith")

	// The call only queues: no post changes yet.
	require.NoError(t, f.keeper.PruneTagReferences(f.ctx, "aurora"))
	require.NoError(t, f.keeper.PruneTagReferences(f.ctx, "aurora"))
	p, err := f.keeper.Post.Get(f.ctx, 1)
	require.NoError(t, err)
	require.Equal(t, []string{"aurora"}, p.Tags)

	// Block 1 reads 200 posts: 1..200.
	require.NoError(t, f.keeper.ProcessTagPruneQueue(f.ctx))
	cursor, err := f.keeper.TagPruneQueue.Get(f.ctx, "aurora")
	require.NoError(t, err)
	require.Equal(t, uint64(201), cursor)
	p, _ = f.keeper.Post.Get(f.ctx, 199)
	require.Empty(t, p.Tags)
	p, _ = f.keeper.Post.Get(f.ctx, 201)
	require.Equal(t, []string{"aurora"}, p.Tags)

	// A second tag queued meanwhile shares the per-block budget.
	require.NoError(t, f.keeper.PruneTagReferences(f.ctx, "zenith"))
	for i := 0; i < 6; i++ {
		require.NoError(t, f.keeper.ProcessTagPruneQueue(f.ctx))
	}
	has, err := f.keeper.TagPruneQueue.Has(f.ctx, "aurora")
	require.NoError(t, err)
	require.False(t, has)
	has, err = f.keeper.TagPruneQueue.Has(f.ctx, "zenith")
	require.NoError(t, err)
	require.False(t, has)
	for i := uint64(1); i <= n; i++ {
		p, err := f.keeper.Post.Get(f.ctx, i)
		require.NoError(t, err)
		if i%2 == 0 {
			require.Equal(t, []string{"phoenix"}, p.Tags, "post %d", i)
		} else {
			require.Empty(t, p.Tags, "post %d", i)
		}
	}

	// Re-created tag: the queue entry is dropped and posts keep the tag.
	require.NoError(t, f.keeper.Post.Set(f.ctx, n+1, types.Post{PostId: n + 1, Tags: []string{"phoenix"}}))
	if f.repKeeper.tags == nil {
		f.repKeeper.tags = map[string]reptypes.Tag{}
	}
	f.repKeeper.tags["phoenix"] = reptypes.Tag{Name: "phoenix"}
	require.NoError(t, f.keeper.PruneTagReferences(f.ctx, "phoenix"))
	require.NoError(t, f.keeper.ProcessTagPruneQueue(f.ctx))
	has, err = f.keeper.TagPruneQueue.Has(f.ctx, "phoenix")
	require.NoError(t, err)
	require.False(t, has)
	p, _ = f.keeper.Post.Get(f.ctx, n+1)
	require.Equal(t, []string{"phoenix"}, p.Tags)
}

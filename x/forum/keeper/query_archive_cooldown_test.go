package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/types"
)

func TestQueryArchiveCooldown(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	t.Run("nil request", func(t *testing.T) {
		_, err := qs.ArchiveCooldown(f.ctx, nil)
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		require.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("zero thread_id", func(t *testing.T) {
		_, err := qs.ArchiveCooldown(f.ctx, &types.QueryArchiveCooldownRequest{RootId: 0})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		require.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("thread not archived", func(t *testing.T) {
		post := f.createTestPost(t, testCreator, 0, 0)

		resp, err := qs.ArchiveCooldown(f.ctx, &types.QueryArchiveCooldownRequest{RootId: post.PostId})
		require.NoError(t, err)
		require.False(t, resp.InCooldown)
	})

	t.Run("archived thread in cooldown", func(t *testing.T) {
		post := f.createTestPost(t, testCreator, 0, 0)
		now := f.sdkCtx().BlockTime().Unix()

		// Create archive metadata with recent archive time
		metadata := types.ArchiveMetadata{
			RootId:          post.PostId,
			ArchiveCount:    1,
			FirstArchivedAt: now,
			LastArchivedAt:  now,
		}
		f.keeper.ArchiveMetadata.Set(f.ctx, post.PostId, metadata)

		resp, err := qs.ArchiveCooldown(f.ctx, &types.QueryArchiveCooldownRequest{RootId: post.PostId})
		require.NoError(t, err)
		require.True(t, resp.InCooldown)
	})
}

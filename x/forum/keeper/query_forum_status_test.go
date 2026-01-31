package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/types"
)

func TestQueryForumStatus(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	t.Run("nil request", func(t *testing.T) {
		_, err := qs.ForumStatus(f.ctx, nil)
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		require.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("default status", func(t *testing.T) {
		resp, err := qs.ForumStatus(f.ctx, &types.QueryForumStatusRequest{})
		require.NoError(t, err)
		require.False(t, resp.ForumPaused)
		require.False(t, resp.ModerationPaused)
		require.GreaterOrEqual(t, resp.CurrentEpoch, int64(0))
	})

	t.Run("forum paused", func(t *testing.T) {
		params := types.DefaultParams()
		params.ForumPaused = true
		f.keeper.Params.Set(f.ctx, params)

		resp, err := qs.ForumStatus(f.ctx, &types.QueryForumStatusRequest{})
		require.NoError(t, err)
		require.True(t, resp.ForumPaused)
		require.False(t, resp.ModerationPaused)
	})

	t.Run("moderation paused", func(t *testing.T) {
		params := types.DefaultParams()
		params.ModerationPaused = true
		f.keeper.Params.Set(f.ctx, params)

		resp, err := qs.ForumStatus(f.ctx, &types.QueryForumStatusRequest{})
		require.NoError(t, err)
		require.False(t, resp.ForumPaused)
		require.True(t, resp.ModerationPaused)
	})

	t.Run("both paused", func(t *testing.T) {
		params := types.DefaultParams()
		params.ForumPaused = true
		params.ModerationPaused = true
		f.keeper.Params.Set(f.ctx, params)

		resp, err := qs.ForumStatus(f.ctx, &types.QueryForumStatusRequest{})
		require.NoError(t, err)
		require.True(t, resp.ForumPaused)
		require.True(t, resp.ModerationPaused)
	})
}

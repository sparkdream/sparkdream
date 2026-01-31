package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/types"
)

func TestQueryTagExists(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	t.Run("nil request", func(t *testing.T) {
		_, err := qs.TagExists(f.ctx, nil)
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		require.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("empty tag name", func(t *testing.T) {
		_, err := qs.TagExists(f.ctx, &types.QueryTagExistsRequest{TagName: ""})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		require.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("tag does not exist", func(t *testing.T) {
		resp, err := qs.TagExists(f.ctx, &types.QueryTagExistsRequest{TagName: "nonexistent"})
		require.NoError(t, err)
		require.False(t, resp.Exists)
		require.Equal(t, int64(0), resp.ExpirationTime)
	})

	t.Run("tag exists", func(t *testing.T) {
		tag := f.createTestTag(t, "test-tag")

		resp, err := qs.TagExists(f.ctx, &types.QueryTagExistsRequest{TagName: tag.Name})
		require.NoError(t, err)
		require.True(t, resp.Exists)
	})

	t.Run("tag with expiration", func(t *testing.T) {
		tag := types.Tag{
			Name:            "expiring-tag",
			CreatedAt:       f.sdkCtx().BlockTime().Unix(),
			ExpirationIndex: f.sdkCtx().BlockTime().Unix() + 86400,
		}
		f.keeper.Tag.Set(f.ctx, tag.Name, tag)

		resp, err := qs.TagExists(f.ctx, &types.QueryTagExistsRequest{TagName: tag.Name})
		require.NoError(t, err)
		require.True(t, resp.Exists)
		require.Equal(t, tag.ExpirationIndex, resp.ExpirationTime)
	})
}

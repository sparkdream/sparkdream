package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/types"
)

func TestQueryArchivedThreadMeta(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	t.Run("nil request", func(t *testing.T) {
		_, err := qs.ArchivedThreadMeta(f.ctx, nil)
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		require.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("zero root_id", func(t *testing.T) {
		_, err := qs.ArchivedThreadMeta(f.ctx, &types.QueryArchivedThreadMetaRequest{RootId: 0})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		require.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("not found", func(t *testing.T) {
		_, err := qs.ArchivedThreadMeta(f.ctx, &types.QueryArchivedThreadMetaRequest{RootId: 999})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		require.Equal(t, codes.NotFound, st.Code())
	})

	t.Run("success", func(t *testing.T) {
		now := f.sdkCtx().BlockTime().Unix()

		// Create archived thread
		archive := types.ArchivedThread{
			RootId:         1,
			PostCount:      10,
			ArchivedAt:     now,
			CompressedData: []byte("compressed"),
		}
		f.keeper.ArchivedThread.Set(f.ctx, 1, archive)

		resp, err := qs.ArchivedThreadMeta(f.ctx, &types.QueryArchivedThreadMetaRequest{RootId: 1})
		require.NoError(t, err)
		require.Equal(t, uint64(1), resp.RootId)
		require.Equal(t, uint64(10), resp.PostCount)
		require.Equal(t, now, resp.ArchivedAt)
	})
}

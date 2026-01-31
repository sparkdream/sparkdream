package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/types"
)

func TestQueryArchivedThreads(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	t.Run("nil request", func(t *testing.T) {
		_, err := qs.ArchivedThreads(f.ctx, nil)
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		require.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("no archives", func(t *testing.T) {
		resp, err := qs.ArchivedThreads(f.ctx, &types.QueryArchivedThreadsRequest{})
		require.NoError(t, err)
		require.Equal(t, uint64(0), resp.RootId)
	})

	t.Run("has archives", func(t *testing.T) {
		now := f.sdkCtx().BlockTime().Unix()

		// Create archived thread
		archive := types.ArchivedThread{
			RootId:         42,
			PostCount:      25,
			ArchivedAt:     now,
			CompressedData: []byte("compressed"),
		}
		f.keeper.ArchivedThread.Set(f.ctx, 42, archive)

		resp, err := qs.ArchivedThreads(f.ctx, &types.QueryArchivedThreadsRequest{})
		require.NoError(t, err)
		require.Equal(t, uint64(42), resp.RootId)
		require.Equal(t, uint64(25), resp.PostCount)
		require.Equal(t, now, resp.ArchivedAt)
	})

	t.Run("multiple archives returns first", func(t *testing.T) {
		now := f.sdkCtx().BlockTime().Unix()

		// Create multiple archived threads
		archive1 := types.ArchivedThread{
			RootId:         1,
			PostCount:      5,
			ArchivedAt:     now,
			CompressedData: []byte("compressed1"),
		}
		archive2 := types.ArchivedThread{
			RootId:         2,
			PostCount:      10,
			ArchivedAt:     now,
			CompressedData: []byte("compressed2"),
		}
		f.keeper.ArchivedThread.Set(f.ctx, 1, archive1)
		f.keeper.ArchivedThread.Set(f.ctx, 2, archive2)

		resp, err := qs.ArchivedThreads(f.ctx, &types.QueryArchivedThreadsRequest{})
		require.NoError(t, err)
		// Should return first one (id=1)
		require.Equal(t, uint64(1), resp.RootId)
	})
}

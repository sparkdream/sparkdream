package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/types"
)

func TestQueryThread(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	t.Run("nil request", func(t *testing.T) {
		_, err := qs.Thread(f.ctx, nil)
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		require.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("thread not found", func(t *testing.T) {
		_, err := qs.Thread(f.ctx, &types.QueryThreadRequest{RootId: 999})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		require.Equal(t, codes.NotFound, st.Code())
	})

	t.Run("not a root post", func(t *testing.T) {
		rootPost := f.createTestPost(t, testCreator, 0, 0)
		reply := f.createTestPost(t, testCreator, rootPost.PostId, 0)

		_, err := qs.Thread(f.ctx, &types.QueryThreadRequest{RootId: reply.PostId})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		require.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("returns root and replies", func(t *testing.T) {
		root := f.createTestPost(t, testCreator, 0, 0)
		reply1 := f.createTestPost(t, testCreator, root.PostId, 0)
		reply2 := f.createTestPost(t, testCreator, root.PostId, 0)

		resp, err := qs.Thread(f.ctx, &types.QueryThreadRequest{RootId: root.PostId})
		require.NoError(t, err)

		ids := map[uint64]bool{}
		for _, p := range resp.Posts {
			ids[p.PostId] = true
		}
		require.True(t, ids[root.PostId], "root not in thread")
		require.True(t, ids[reply1.PostId], "reply1 not in thread")
		require.True(t, ids[reply2.PostId], "reply2 not in thread")
	})
}

package keeper_test

import (
	"testing"

	"sparkdream/x/blog/keeper"
	"sparkdream/x/blog/types"

	"github.com/stretchr/testify/require"
)

func TestAppendReply(t *testing.T) {
	f := initFixture(t)

	// Counter starts at 0
	require.Equal(t, uint64(0), f.keeper.GetReplyCount(f.ctx))

	// First append returns ID 0
	id := f.keeper.AppendReply(f.ctx, types.Reply{
		Creator: "creator1",
		PostId:  10,
		Body:    "Reply body",
	})
	require.Equal(t, uint64(0), id)
	require.Equal(t, uint64(1), f.keeper.GetReplyCount(f.ctx))

	// Verify stored
	reply, found := f.keeper.GetReply(f.ctx, 0)
	require.True(t, found)
	require.Equal(t, uint64(0), reply.Id)
	require.Equal(t, "creator1", reply.Creator)
	require.Equal(t, uint64(10), reply.PostId)

	// Second append returns ID 1
	id2 := f.keeper.AppendReply(f.ctx, types.Reply{
		Creator: "creator2",
		PostId:  10,
		Body:    "Reply 2",
	})
	require.Equal(t, uint64(1), id2)
	require.Equal(t, uint64(2), f.keeper.GetReplyCount(f.ctx))
}

func TestAppendReply_AutoIncrement(t *testing.T) {
	f := initFixture(t)

	ids := make([]uint64, 5)
	for i := 0; i < 5; i++ {
		ids[i] = f.keeper.AppendReply(f.ctx, types.Reply{
			Creator: "creator",
			PostId:  1,
		})
	}

	for i, id := range ids {
		require.Equal(t, uint64(i), id)
	}
	require.Equal(t, uint64(5), f.keeper.GetReplyCount(f.ctx))
}

func TestSetReply(t *testing.T) {
	f := initFixture(t)

	f.keeper.AppendReply(f.ctx, types.Reply{
		Creator: "creator",
		PostId:  1,
		Body:    "Original",
	})

	// Update via SetReply
	f.keeper.SetReply(f.ctx, types.Reply{
		Id:      0,
		Creator: "creator",
		PostId:  1,
		Body:    "Updated body",
	})

	reply, found := f.keeper.GetReply(f.ctx, 0)
	require.True(t, found)
	require.Equal(t, "Updated body", reply.Body)
}

func TestGetReply_NotFound(t *testing.T) {
	f := initFixture(t)

	_, found := f.keeper.GetReply(f.ctx, 999)
	require.False(t, found)
}

func TestRemoveReply(t *testing.T) {
	f := initFixture(t)

	f.keeper.AppendReply(f.ctx, types.Reply{
		Creator: "creator",
		PostId:  1,
		Body:    "To delete",
	})

	_, found := f.keeper.GetReply(f.ctx, 0)
	require.True(t, found)

	f.keeper.RemoveReply(f.ctx, 0)

	_, found = f.keeper.GetReply(f.ctx, 0)
	require.False(t, found)
}

func TestRemoveReply_NonExistent(t *testing.T) {
	f := initFixture(t)

	// Should not panic
	f.keeper.RemoveReply(f.ctx, 999)
}

func TestReplyCount_SetAndGet(t *testing.T) {
	f := initFixture(t)

	require.Equal(t, uint64(0), f.keeper.GetReplyCount(f.ctx))

	f.keeper.SetReplyCount(f.ctx, 42)
	require.Equal(t, uint64(42), f.keeper.GetReplyCount(f.ctx))

	f.keeper.SetReplyCount(f.ctx, 0)
	require.Equal(t, uint64(0), f.keeper.GetReplyCount(f.ctx))
}

func TestGetReplyIDBytes(t *testing.T) {
	// Zero
	b := keeper.GetReplyIDBytes(0)
	require.Len(t, b, 8)
	require.Equal(t, []byte{0, 0, 0, 0, 0, 0, 0, 0}, b)

	// One
	b = keeper.GetReplyIDBytes(1)
	require.Equal(t, []byte{0, 0, 0, 0, 0, 0, 0, 1}, b)

	// 256
	b = keeper.GetReplyIDBytes(256)
	require.Equal(t, []byte{0, 0, 0, 0, 0, 0, 1, 0}, b)

	// Max
	b = keeper.GetReplyIDBytes(^uint64(0))
	require.Equal(t, []byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}, b)
}

func TestAppendReply_PostIndex(t *testing.T) {
	f := initFixture(t)

	// Append replies to different posts
	f.keeper.AppendReply(f.ctx, types.Reply{Creator: "a", PostId: 10, Body: "R1"})
	f.keeper.AppendReply(f.ctx, types.Reply{Creator: "b", PostId: 20, Body: "R2"})
	f.keeper.AppendReply(f.ctx, types.Reply{Creator: "c", PostId: 10, Body: "R3"})

	// All retrievable by ID
	r0, found := f.keeper.GetReply(f.ctx, 0)
	require.True(t, found)
	require.Equal(t, uint64(10), r0.PostId)

	r1, found := f.keeper.GetReply(f.ctx, 1)
	require.True(t, found)
	require.Equal(t, uint64(20), r1.PostId)

	r2, found := f.keeper.GetReply(f.ctx, 2)
	require.True(t, found)
	require.Equal(t, uint64(10), r2.PostId)
}

func TestReplyCountIndependentFromPostCount(t *testing.T) {
	f := initFixture(t)

	// Append posts and replies — counters should be independent
	f.keeper.AppendPost(f.ctx, types.Post{Creator: "c", Title: "P1"})
	f.keeper.AppendPost(f.ctx, types.Post{Creator: "c", Title: "P2"})
	f.keeper.AppendReply(f.ctx, types.Reply{Creator: "c", PostId: 0})

	require.Equal(t, uint64(2), f.keeper.GetPostCount(f.ctx))
	require.Equal(t, uint64(1), f.keeper.GetReplyCount(f.ctx))
}

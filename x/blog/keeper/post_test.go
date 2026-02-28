package keeper_test

import (
	"testing"

	"sparkdream/x/blog/keeper"
	"sparkdream/x/blog/types"

	"github.com/stretchr/testify/require"
)

func TestAppendPost(t *testing.T) {
	f := initFixture(t)

	// Counter starts at 0
	require.Equal(t, uint64(0), f.keeper.GetPostCount(f.ctx))

	// First append returns ID 0
	id := f.keeper.AppendPost(f.ctx, types.Post{
		Creator: "creator1",
		Title:   "First",
		Body:    "Body 1",
	})
	require.Equal(t, uint64(0), id)
	require.Equal(t, uint64(1), f.keeper.GetPostCount(f.ctx))

	// Verify stored
	post, found := f.keeper.GetPost(f.ctx, 0)
	require.True(t, found)
	require.Equal(t, uint64(0), post.Id)
	require.Equal(t, "creator1", post.Creator)
	require.Equal(t, "First", post.Title)

	// Second append returns ID 1
	id2 := f.keeper.AppendPost(f.ctx, types.Post{
		Creator: "creator2",
		Title:   "Second",
		Body:    "Body 2",
	})
	require.Equal(t, uint64(1), id2)
	require.Equal(t, uint64(2), f.keeper.GetPostCount(f.ctx))
}

func TestAppendPost_AutoIncrement(t *testing.T) {
	f := initFixture(t)

	ids := make([]uint64, 5)
	for i := 0; i < 5; i++ {
		ids[i] = f.keeper.AppendPost(f.ctx, types.Post{
			Creator: "creator",
			Title:   "Post",
		})
	}

	for i, id := range ids {
		require.Equal(t, uint64(i), id)
	}
	require.Equal(t, uint64(5), f.keeper.GetPostCount(f.ctx))
}

func TestSetPost(t *testing.T) {
	f := initFixture(t)

	// Append a post
	f.keeper.AppendPost(f.ctx, types.Post{
		Creator: "creator",
		Title:   "Original",
		Body:    "Original body",
	})

	// Update via SetPost
	f.keeper.SetPost(f.ctx, types.Post{
		Id:      0,
		Creator: "creator",
		Title:   "Updated",
		Body:    "Updated body",
	})

	post, found := f.keeper.GetPost(f.ctx, 0)
	require.True(t, found)
	require.Equal(t, "Updated", post.Title)
	require.Equal(t, "Updated body", post.Body)
}

func TestGetPost_NotFound(t *testing.T) {
	f := initFixture(t)

	_, found := f.keeper.GetPost(f.ctx, 999)
	require.False(t, found)
}

func TestRemovePost(t *testing.T) {
	f := initFixture(t)

	f.keeper.AppendPost(f.ctx, types.Post{
		Creator: "creator",
		Title:   "To delete",
	})

	// Verify exists
	_, found := f.keeper.GetPost(f.ctx, 0)
	require.True(t, found)

	// Remove
	f.keeper.RemovePost(f.ctx, 0)

	// Verify gone
	_, found = f.keeper.GetPost(f.ctx, 0)
	require.False(t, found)
}

func TestRemovePost_NonExistent(t *testing.T) {
	f := initFixture(t)

	// Should not panic when removing non-existent post
	f.keeper.RemovePost(f.ctx, 999)
}

func TestPostCount_SetAndGet(t *testing.T) {
	f := initFixture(t)

	require.Equal(t, uint64(0), f.keeper.GetPostCount(f.ctx))

	f.keeper.SetPostCount(f.ctx, 42)
	require.Equal(t, uint64(42), f.keeper.GetPostCount(f.ctx))

	f.keeper.SetPostCount(f.ctx, 0)
	require.Equal(t, uint64(0), f.keeper.GetPostCount(f.ctx))
}

func TestGetPostIDBytes(t *testing.T) {
	// Zero
	b := keeper.GetPostIDBytes(0)
	require.Len(t, b, 8)
	require.Equal(t, []byte{0, 0, 0, 0, 0, 0, 0, 0}, b)

	// One
	b = keeper.GetPostIDBytes(1)
	require.Equal(t, []byte{0, 0, 0, 0, 0, 0, 0, 1}, b)

	// 256
	b = keeper.GetPostIDBytes(256)
	require.Equal(t, []byte{0, 0, 0, 0, 0, 0, 1, 0}, b)

	// Max
	b = keeper.GetPostIDBytes(^uint64(0))
	require.Equal(t, []byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}, b)
}

func TestAppendPost_CreatorIndex(t *testing.T) {
	f := initFixture(t)

	// Append posts from two different creators
	f.keeper.AppendPost(f.ctx, types.Post{Creator: "alice", Title: "A1"})
	f.keeper.AppendPost(f.ctx, types.Post{Creator: "bob", Title: "B1"})
	f.keeper.AppendPost(f.ctx, types.Post{Creator: "alice", Title: "A2"})

	// Both should be retrievable by ID
	p0, found := f.keeper.GetPost(f.ctx, 0)
	require.True(t, found)
	require.Equal(t, "alice", p0.Creator)

	p1, found := f.keeper.GetPost(f.ctx, 1)
	require.True(t, found)
	require.Equal(t, "bob", p1.Creator)

	p2, found := f.keeper.GetPost(f.ctx, 2)
	require.True(t, found)
	require.Equal(t, "alice", p2.Creator)
}

package keeper_test

import (
	"fmt"
	"testing"

	"sparkdream/x/forum/types"

	"github.com/stretchr/testify/require"
)

func TestFollowThread(t *testing.T) {
	f := initFixture(t)

	// Create a category and thread
	cat := f.createTestCategory(t, "General")
	thread := f.createTestPost(t, testCreator, 0, cat.CategoryId)

	tests := []struct {
		name        string
		msg         *types.MsgFollowThread
		setup       func()
		expectError bool
		errContains string
	}{
		{
			name: "successful follow",
			msg: &types.MsgFollowThread{
				Creator:  testCreator2,
				ThreadId: thread.PostId,
			},
			expectError: false,
		},
		{
			name: "invalid creator address",
			msg: &types.MsgFollowThread{
				Creator:  "invalid-address",
				ThreadId: thread.PostId,
			},
			expectError: true,
			errContains: "invalid creator address",
		},
		{
			name: "thread not found",
			msg: &types.MsgFollowThread{
				Creator:  testCreator2,
				ThreadId: 9999,
			},
			expectError: true,
			errContains: "not found",
		},
		{
			name: "not a root post",
			msg: &types.MsgFollowThread{
				Creator:  testCreator2,
				ThreadId: thread.PostId,
			},
			setup: func() {
				// Create a reply and try to follow it
				reply := f.createTestPost(t, testCreator, thread.PostId, cat.CategoryId)
				// We'll test this case separately
				_ = reply
			},
			expectError: true,
			errContains: "can only follow root posts",
		},
		{
			name: "already following",
			msg: &types.MsgFollowThread{
				Creator:  testCreator2,
				ThreadId: thread.PostId,
			},
			setup: func() {
				// Create existing follow
				followKey := fmt.Sprintf("%s:%d", testCreator2, thread.PostId)
				follow := types.ThreadFollow{
					ThreadId:   thread.PostId,
					Follower:   testCreator2,
					FollowedAt: f.sdkCtx().BlockTime().Unix(),
				}
				_ = f.keeper.ThreadFollow.Set(f.ctx, followKey, follow)
			},
			expectError: true,
			errContains: "already following",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clean up follows
			followKey := fmt.Sprintf("%s:%d", testCreator2, thread.PostId)
			_ = f.keeper.ThreadFollow.Remove(f.ctx, followKey)

			// Reset the thread to be a root post
			p, _ := f.keeper.Post.Get(f.ctx, thread.PostId)
			p.ParentId = 0
			_ = f.keeper.Post.Set(f.ctx, thread.PostId, p)

			// Handle "not a root post" test differently
			if tt.name == "not a root post" {
				reply := f.createTestPost(t, testCreator, thread.PostId, cat.CategoryId)
				_, err := f.msgServer.FollowThread(f.ctx, &types.MsgFollowThread{
					Creator:  testCreator2,
					ThreadId: reply.PostId,
				})
				require.Error(t, err)
				require.Contains(t, err.Error(), "can only follow root posts")
				return
			}

			if tt.setup != nil {
				tt.setup()
			}

			resp, err := f.msgServer.FollowThread(f.ctx, tt.msg)

			if tt.expectError {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.errContains)
			} else {
				require.NoError(t, err)
				require.NotNil(t, resp)

				// Verify follow was created
				followKey := fmt.Sprintf("%s:%d", tt.msg.Creator, tt.msg.ThreadId)
				follow, err := f.keeper.ThreadFollow.Get(f.ctx, followKey)
				require.NoError(t, err)
				require.Equal(t, tt.msg.Creator, follow.Follower)
				require.Equal(t, tt.msg.ThreadId, follow.ThreadId)

				// Verify follow count was updated
				count, err := f.keeper.ThreadFollowCount.Get(f.ctx, tt.msg.ThreadId)
				require.NoError(t, err)
				require.Equal(t, uint64(1), count.FollowerCount)
			}
		})
	}
}

func TestFollowThreadRateLimit(t *testing.T) {
	f := initFixture(t)

	// Create a category and multiple threads
	cat := f.createTestCategory(t, "General")
	var threads []types.Post
	for i := 0; i < int(types.DefaultMaxFollowsPerDay)+5; i++ {
		thread := f.createTestPost(t, testCreator, 0, cat.CategoryId)
		threads = append(threads, thread)
	}

	// Follow up to the limit
	for i := 0; i < int(types.DefaultMaxFollowsPerDay); i++ {
		_, err := f.msgServer.FollowThread(f.ctx, &types.MsgFollowThread{
			Creator:  testCreator2,
			ThreadId: threads[i].PostId,
		})
		require.NoError(t, err, "should succeed for follow %d", i+1)
	}

	// Next follow should fail due to rate limit
	_, err := f.msgServer.FollowThread(f.ctx, &types.MsgFollowThread{
		Creator:  testCreator2,
		ThreadId: threads[types.DefaultMaxFollowsPerDay].PostId,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "rate limit exceeded")
}

func TestUnfollowThread(t *testing.T) {
	f := initFixture(t)

	// Create a category and thread
	cat := f.createTestCategory(t, "General")
	thread := f.createTestPost(t, testCreator, 0, cat.CategoryId)

	// First follow the thread
	_, err := f.msgServer.FollowThread(f.ctx, &types.MsgFollowThread{
		Creator:  testCreator2,
		ThreadId: thread.PostId,
	})
	require.NoError(t, err)

	// Verify follow count is 1
	count, err := f.keeper.ThreadFollowCount.Get(f.ctx, thread.PostId)
	require.NoError(t, err)
	require.Equal(t, uint64(1), count.FollowerCount)

	// Unfollow
	_, err = f.msgServer.UnfollowThread(f.ctx, &types.MsgUnfollowThread{
		Creator:  testCreator2,
		ThreadId: thread.PostId,
	})
	require.NoError(t, err)

	// Verify follow was removed
	followKey := fmt.Sprintf("%s:%d", testCreator2, thread.PostId)
	_, err = f.keeper.ThreadFollow.Get(f.ctx, followKey)
	require.Error(t, err) // Should not find

	// Verify follow count was decremented
	count, err = f.keeper.ThreadFollowCount.Get(f.ctx, thread.PostId)
	require.NoError(t, err)
	require.Equal(t, uint64(0), count.FollowerCount)
}

func TestUnfollowThreadNotFollowing(t *testing.T) {
	f := initFixture(t)

	// Create a category and thread
	cat := f.createTestCategory(t, "General")
	thread := f.createTestPost(t, testCreator, 0, cat.CategoryId)

	// Try to unfollow without following first
	_, err := f.msgServer.UnfollowThread(f.ctx, &types.MsgUnfollowThread{
		Creator:  testCreator2,
		ThreadId: thread.PostId,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "not following")
}

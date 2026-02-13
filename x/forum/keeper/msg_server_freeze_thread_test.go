package keeper_test

import (
	"testing"

	"sparkdream/x/forum/types"

	"github.com/stretchr/testify/require"
)

func TestFreezeThread(t *testing.T) {
	f := initFixture(t)

	// Create a category and thread
	cat := f.createTestCategory(t, "General")
	thread := f.createTestPost(t, testCreator, 0, cat.CategoryId)

	// Make the thread inactive (old enough to archive)
	p, _ := f.keeper.Post.Get(f.ctx, thread.PostId)
	p.CreatedAt = f.sdkCtx().BlockTime().Unix() - types.DefaultArchiveThreshold - 1
	_ = f.keeper.Post.Set(f.ctx, thread.PostId, p)

	tests := []struct {
		name        string
		msg         *types.MsgFreezeThread
		setup       func()
		expectError bool
		errContains string
	}{
		{
			name: "successful archive",
			msg: &types.MsgFreezeThread{
				Creator: testCreator,
				RootId:  thread.PostId,
			},
			expectError: false,
		},
		{
			name: "invalid creator address",
			msg: &types.MsgFreezeThread{
				Creator: "invalid-address",
				RootId:  thread.PostId,
			},
			expectError: true,
			errContains: "invalid creator address",
		},
		{
			name: "forum paused",
			msg: &types.MsgFreezeThread{
				Creator: testCreator,
				RootId:  thread.PostId,
			},
			setup: func() {
				params := types.DefaultParams()
				params.ForumPaused = true
				_ = f.keeper.Params.Set(f.ctx, params)
			},
			expectError: true,
			errContains: "forum is paused",
		},
		{
			name: "thread not found",
			msg: &types.MsgFreezeThread{
				Creator: testCreator,
				RootId:  9999,
			},
			expectError: true,
			errContains: "not found",
		},
		{
			name: "only allowed on root posts",
			msg: &types.MsgFreezeThread{
				Creator: testCreator,
				RootId:  thread.PostId,
			},
			setup: func() {
				// Make the post a reply (non-root)
				p, _ := f.keeper.Post.Get(f.ctx, thread.PostId)
				p.ParentId = 1
				_ = f.keeper.Post.Set(f.ctx, thread.PostId, p)
			},
			expectError: true,
			errContains: "only allowed on root posts",
		},
		{
			name: "thread too recent",
			msg: &types.MsgFreezeThread{
				Creator: testCreator,
				RootId:  thread.PostId,
			},
			setup: func() {
				// Make the thread recent
				p, _ := f.keeper.Post.Get(f.ctx, thread.PostId)
				p.CreatedAt = f.sdkCtx().BlockTime().Unix()
				_ = f.keeper.Post.Set(f.ctx, thread.PostId, p)
			},
			expectError: true,
			errContains: "must be inactive",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset params and thread state
			_ = f.keeper.Params.Set(f.ctx, types.DefaultParams())
			p, _ := f.keeper.Post.Get(f.ctx, thread.PostId)
			p.ParentId = 0
			p.Status = types.PostStatus_POST_STATUS_ACTIVE
			p.CreatedAt = f.sdkCtx().BlockTime().Unix() - types.DefaultArchiveThreshold - 1
			_ = f.keeper.Post.Set(f.ctx, thread.PostId, p)

			// Remove archive metadata if exists
			_ = f.keeper.ArchiveMetadata.Remove(f.ctx, thread.PostId)

			if tt.setup != nil {
				tt.setup()
			}

			resp, err := f.msgServer.FreezeThread(f.ctx, tt.msg)

			if tt.expectError {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.errContains)
			} else {
				require.NoError(t, err)
				require.NotNil(t, resp)

				// Verify root post status is ARCHIVED
				post, err := f.keeper.Post.Get(f.ctx, thread.PostId)
				require.NoError(t, err)
				require.Equal(t, types.PostStatus_POST_STATUS_ARCHIVED, post.Status)

				// Verify archive metadata was created
				meta, err := f.keeper.ArchiveMetadata.Get(f.ctx, thread.PostId)
				require.NoError(t, err)
				require.Equal(t, uint64(1), meta.ArchiveCount)
			}
		})
	}
}

func TestFreezeThreadWithReplies(t *testing.T) {
	f := initFixture(t)

	// Create a category, thread, and replies
	cat := f.createTestCategory(t, "General")
	thread := f.createTestPost(t, testCreator, 0, cat.CategoryId)

	// Make thread old
	p, _ := f.keeper.Post.Get(f.ctx, thread.PostId)
	p.CreatedAt = f.sdkCtx().BlockTime().Unix() - types.DefaultArchiveThreshold - 1
	_ = f.keeper.Post.Set(f.ctx, thread.PostId, p)

	// Create some replies
	reply1 := f.createTestPost(t, testCreator2, thread.PostId, cat.CategoryId)
	reply2 := f.createTestPost(t, testSentinel, thread.PostId, cat.CategoryId)

	// Archive
	resp, err := f.msgServer.FreezeThread(f.ctx, &types.MsgFreezeThread{
		Creator: testCreator,
		RootId:  thread.PostId,
	})
	require.NoError(t, err)
	require.NotNil(t, resp)

	// Verify all posts have ARCHIVED status (posts stay in store)
	rootPost, err := f.keeper.Post.Get(f.ctx, thread.PostId)
	require.NoError(t, err)
	require.Equal(t, types.PostStatus_POST_STATUS_ARCHIVED, rootPost.Status)

	r1, err := f.keeper.Post.Get(f.ctx, reply1.PostId)
	require.NoError(t, err)
	require.Equal(t, types.PostStatus_POST_STATUS_ARCHIVED, r1.Status)

	r2, err := f.keeper.Post.Get(f.ctx, reply2.PostId)
	require.NoError(t, err)
	require.Equal(t, types.PostStatus_POST_STATUS_ARCHIVED, r2.Status)
}

func TestFreezeThreadWithPendingAppeal(t *testing.T) {
	f := initFixture(t)

	// Create a category and thread
	cat := f.createTestCategory(t, "General")
	thread := f.createTestPost(t, testCreator, 0, cat.CategoryId)

	// Make thread old
	p, _ := f.keeper.Post.Get(f.ctx, thread.PostId)
	p.CreatedAt = f.sdkCtx().BlockTime().Unix() - types.DefaultArchiveThreshold - 1
	_ = f.keeper.Post.Set(f.ctx, thread.PostId, p)

	// Create a pending lock appeal
	lockRecord := types.ThreadLockRecord{
		RootId:        thread.PostId,
		Sentinel:      testSentinel,
		AppealPending: true,
	}
	_ = f.keeper.ThreadLockRecord.Set(f.ctx, thread.PostId, lockRecord)

	// Try to archive - should fail
	_, err := f.msgServer.FreezeThread(f.ctx, &types.MsgFreezeThread{
		Creator: testCreator,
		RootId:  thread.PostId,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "pending appeal")
}

func TestFreezeThreadAlreadyArchived(t *testing.T) {
	f := initFixture(t)

	// Create a category and thread
	cat := f.createTestCategory(t, "General")
	thread := f.createTestPost(t, testCreator, 0, cat.CategoryId)

	// Set the post status to ARCHIVED
	p, _ := f.keeper.Post.Get(f.ctx, thread.PostId)
	p.CreatedAt = f.sdkCtx().BlockTime().Unix() - types.DefaultArchiveThreshold - 1
	p.Status = types.PostStatus_POST_STATUS_ARCHIVED
	_ = f.keeper.Post.Set(f.ctx, thread.PostId, p)

	// Try to archive again - should fail
	_, err := f.msgServer.FreezeThread(f.ctx, &types.MsgFreezeThread{
		Creator: testCreator,
		RootId:  thread.PostId,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "archived")
}

func TestUnarchiveThread(t *testing.T) {
	f := initFixture(t)

	// Create a category and thread
	cat := f.createTestCategory(t, "General")
	thread := f.createTestPost(t, testCreator, 0, cat.CategoryId)

	// Make thread old enough to archive
	p, _ := f.keeper.Post.Get(f.ctx, thread.PostId)
	p.CreatedAt = f.sdkCtx().BlockTime().Unix() - types.DefaultArchiveThreshold - 1
	_ = f.keeper.Post.Set(f.ctx, thread.PostId, p)

	// Archive it
	_, err := f.msgServer.FreezeThread(f.ctx, &types.MsgFreezeThread{
		Creator: testCreator,
		RootId:  thread.PostId,
	})
	require.NoError(t, err)

	// Verify it's archived (status flag, post still exists)
	archivedPost, err := f.keeper.Post.Get(f.ctx, thread.PostId)
	require.NoError(t, err)
	require.Equal(t, types.PostStatus_POST_STATUS_ARCHIVED, archivedPost.Status)

	// Update archive metadata time to be past cooldown
	meta, _ := f.keeper.ArchiveMetadata.Get(f.ctx, thread.PostId)
	meta.LastArchivedAt = f.sdkCtx().BlockTime().Unix() - types.DefaultUnarchiveCooldown - 1
	_ = f.keeper.ArchiveMetadata.Set(f.ctx, thread.PostId, meta)

	// Unarchive
	_, err = f.msgServer.UnarchiveThread(f.ctx, &types.MsgUnarchiveThread{
		Creator: testCreator,
		RootId:  thread.PostId,
	})
	require.NoError(t, err)

	// Verify post status was restored to ACTIVE
	restored, err := f.keeper.Post.Get(f.ctx, thread.PostId)
	require.NoError(t, err)
	require.Equal(t, types.PostStatus_POST_STATUS_ACTIVE, restored.Status)
	require.Equal(t, testCreator, restored.Author)
}

func TestUnarchiveThreadCooldown(t *testing.T) {
	f := initFixture(t)

	// Create a category and thread
	cat := f.createTestCategory(t, "General")
	thread := f.createTestPost(t, testCreator, 0, cat.CategoryId)

	// Make thread old enough to archive
	p, _ := f.keeper.Post.Get(f.ctx, thread.PostId)
	p.CreatedAt = f.sdkCtx().BlockTime().Unix() - types.DefaultArchiveThreshold - 1
	_ = f.keeper.Post.Set(f.ctx, thread.PostId, p)

	// Archive it
	_, err := f.msgServer.FreezeThread(f.ctx, &types.MsgFreezeThread{
		Creator: testCreator,
		RootId:  thread.PostId,
	})
	require.NoError(t, err)

	// Try to unarchive immediately - should fail due to cooldown
	_, err = f.msgServer.UnarchiveThread(f.ctx, &types.MsgUnarchiveThread{
		Creator: testCreator,
		RootId:  thread.PostId,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "cooldown")
}

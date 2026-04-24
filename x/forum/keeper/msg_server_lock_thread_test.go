package keeper_test

import (
	"testing"

	"sparkdream/x/forum/types"
	reptypes "sparkdream/x/rep/types"

	"github.com/stretchr/testify/require"
)

func TestLockThread(t *testing.T) {
	f := initFixture(t)

	// Create a category and thread (root post)
	cat := f.createTestCategory(t, "General")
	thread := f.createTestPost(t, testCreator, 0, cat.CategoryId)

	// Create a sentinel with sufficient bond (bond record in x/rep).
	f.createTestSentinel(t, testSentinel, "3000")

	tests := []struct {
		name        string
		msg         *types.MsgLockThread
		setup       func()
		expectError bool
		errContains string
	}{
		{
			name: "successful lock by sentinel",
			msg: &types.MsgLockThread{
				Creator: testSentinel,
				RootId:  thread.PostId,
				Reason:  "Off-topic discussion",
			},
			expectError: false,
		},
		{
			name: "invalid creator address",
			msg: &types.MsgLockThread{
				Creator: "invalid-address",
				RootId:  thread.PostId,
				Reason:  "Test",
			},
			expectError: true,
			errContains: "invalid creator address",
		},
		{
			name: "forum paused",
			msg: &types.MsgLockThread{
				Creator: testSentinel,
				RootId:  thread.PostId,
				Reason:  "Test",
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
			msg: &types.MsgLockThread{
				Creator: testSentinel,
				RootId:  9999,
				Reason:  "Test",
			},
			expectError: true,
			errContains: "post not found",
		},
		{
			name: "only allowed on root posts",
			msg: &types.MsgLockThread{
				Creator: testSentinel,
				RootId:  thread.PostId,
				Reason:  "Test",
			},
			setup: func() {
				// Create a reply and try to lock it
				reply := f.createTestPost(t, testCreator2, thread.PostId, cat.CategoryId)
				// We need to override the msg in this case, so we'll handle it differently
				_ = reply
			},
			expectError: true,
			errContains: "only allowed on root posts",
		},
		{
			name: "thread is already locked",
			msg: &types.MsgLockThread{
				Creator: testSentinel,
				RootId:  thread.PostId,
				Reason:  "Test",
			},
			setup: func() {
				p, _ := f.keeper.Post.Get(f.ctx, thread.PostId)
				p.Locked = true
				_ = f.keeper.Post.Set(f.ctx, thread.PostId, p)
			},
			expectError: true,
			errContains: "thread is already locked",
		},
		{
			name: "moderation paused for sentinel",
			msg: &types.MsgLockThread{
				Creator: testSentinel,
				RootId:  thread.PostId,
				Reason:  "Test",
			},
			setup: func() {
				params := types.DefaultParams()
				params.ModerationPaused = true
				_ = f.keeper.Params.Set(f.ctx, params)
			},
			expectError: true,
			errContains: "moderation is paused",
		},
		{
			name: "not a sentinel",
			msg: &types.MsgLockThread{
				Creator: testCreator2,
				RootId:  thread.PostId,
				Reason:  "Test",
			},
			expectError: true,
			errContains: "not a registered sentinel",
		},
		{
			name: "sentinel missing reason",
			msg: &types.MsgLockThread{
				Creator: testSentinel,
				RootId:  thread.PostId,
				Reason:  "",
			},
			expectError: true,
			errContains: "lock reason required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset params and thread state
			_ = f.keeper.Params.Set(f.ctx, types.DefaultParams())
			p, _ := f.keeper.Post.Get(f.ctx, thread.PostId)
			p.Locked = false
			p.ParentId = 0 // Ensure it's a root post
			_ = f.keeper.Post.Set(f.ctx, thread.PostId, p)

			// Reset sentinel (forum-local counters + rep bond record)
			_ = f.keeper.SentinelActivity.Set(f.ctx, testSentinel, types.SentinelActivity{Address: testSentinel})
			f.repKeeper.sentinels[testSentinel] = reptypes.BondedRole{
				Address:            testSentinel,
				CurrentBond:        "3000",
				TotalCommittedBond: "0",
				BondStatus:         reptypes.BondedRoleStatus_BONDED_ROLE_STATUS_NORMAL,
			}

			// Skip the "only allowed on root posts" test as it requires special handling
			if tt.name == "only allowed on root posts" {
				reply := f.createTestPost(t, testCreator2, thread.PostId, cat.CategoryId)
				_, err := f.msgServer.LockThread(f.ctx, &types.MsgLockThread{
					Creator: testSentinel,
					RootId:  reply.PostId,
					Reason:  "Test",
				})
				require.Error(t, err)
				require.Contains(t, err.Error(), "only allowed on root posts")
				return
			}

			if tt.setup != nil {
				tt.setup()
			}

			resp, err := f.msgServer.LockThread(f.ctx, tt.msg)

			if tt.expectError {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.errContains)
			} else {
				require.NoError(t, err)
				require.NotNil(t, resp)

				// Verify thread was locked
				lockedThread, err := f.keeper.Post.Get(f.ctx, thread.PostId)
				require.NoError(t, err)
				require.True(t, lockedThread.Locked)
				require.Equal(t, tt.msg.Creator, lockedThread.LockedBy)
				require.Equal(t, tt.msg.Reason, lockedThread.LockReason)

				// Verify lock record was created for sentinel
				lockRecord, err := f.keeper.ThreadLockRecord.Get(f.ctx, thread.PostId)
				require.NoError(t, err)
				require.Equal(t, tt.msg.Creator, lockRecord.Sentinel)
			}
		})
	}
}

func TestLockThreadByGovAuthority(t *testing.T) {
	f := initFixture(t)

	// Create a category and thread
	cat := f.createTestCategory(t, "General")
	thread := f.createTestPost(t, testCreator, 0, cat.CategoryId)

	// Get authority address
	authority, _ := f.addressCodec.BytesToString(f.keeper.GetAuthority())

	// Lock by gov authority (no reason required)
	resp, err := f.msgServer.LockThread(f.ctx, &types.MsgLockThread{
		Creator: authority,
		RootId:  thread.PostId,
		Reason:  "", // Optional for gov authority
	})
	require.NoError(t, err)
	require.NotNil(t, resp)

	// Verify thread was locked
	lockedThread, err := f.keeper.Post.Get(f.ctx, thread.PostId)
	require.NoError(t, err)
	require.True(t, lockedThread.Locked)

	// Verify no lock record was created (gov locks don't create lock records)
	_, err = f.keeper.ThreadLockRecord.Get(f.ctx, thread.PostId)
	require.Error(t, err) // Should not find lock record
}

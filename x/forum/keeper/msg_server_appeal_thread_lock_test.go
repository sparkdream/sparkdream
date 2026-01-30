package keeper_test

import (
	"context"
	"testing"

	"sparkdream/x/forum/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

func TestAppealThreadLock(t *testing.T) {
	f := initFixture(t)

	// Create a category and locked thread
	cat := f.createTestCategory(t, "General")
	thread := f.createTestPost(t, testCreator, 0, cat.CategoryId)

	// Lock the thread
	p, _ := f.keeper.Post.Get(f.ctx, thread.PostId)
	p.Locked = true
	p.LockedBy = testSentinel
	_ = f.keeper.Post.Set(f.ctx, thread.PostId, p)

	// Create lock record (sentinel lock)
	lockRecord := types.ThreadLockRecord{
		RootId:        thread.PostId,
		Sentinel:      testSentinel,
		LockedAt:      f.sdkCtx().BlockTime().Unix() - types.DefaultLockAppealCooldown - 1, // Past cooldown
		LockReason:    "Off-topic",
		AppealPending: false,
	}
	_ = f.keeper.ThreadLockRecord.Set(f.ctx, thread.PostId, lockRecord)

	tests := []struct {
		name        string
		msg         *types.MsgAppealThreadLock
		setup       func()
		expectError bool
		errContains string
	}{
		{
			name: "successful appeal",
			msg: &types.MsgAppealThreadLock{
				Creator: testCreator,
				RootId:  thread.PostId,
			},
			expectError: false,
		},
		{
			name: "invalid creator address",
			msg: &types.MsgAppealThreadLock{
				Creator: "invalid-address",
				RootId:  thread.PostId,
			},
			expectError: true,
			errContains: "invalid creator address",
		},
		{
			name: "appeals paused",
			msg: &types.MsgAppealThreadLock{
				Creator: testCreator,
				RootId:  thread.PostId,
			},
			setup: func() {
				params := types.DefaultParams()
				params.AppealsPaused = true
				_ = f.keeper.Params.Set(f.ctx, params)
			},
			expectError: true,
			errContains: "appeals are paused",
		},
		{
			name: "thread not found",
			msg: &types.MsgAppealThreadLock{
				Creator: testCreator,
				RootId:  9999,
			},
			expectError: true,
			errContains: "not found",
		},
		{
			name: "thread not locked",
			msg: &types.MsgAppealThreadLock{
				Creator: testCreator,
				RootId:  thread.PostId,
			},
			setup: func() {
				p, _ := f.keeper.Post.Get(f.ctx, thread.PostId)
				p.Locked = false
				_ = f.keeper.Post.Set(f.ctx, thread.PostId, p)
			},
			expectError: true,
			errContains: "thread is not locked",
		},
		{
			name: "not thread author",
			msg: &types.MsgAppealThreadLock{
				Creator: testCreator2,
				RootId:  thread.PostId,
			},
			expectError: true,
			errContains: "only the thread author",
		},
		{
			name: "appeal already pending",
			msg: &types.MsgAppealThreadLock{
				Creator: testCreator,
				RootId:  thread.PostId,
			},
			setup: func() {
				lr, _ := f.keeper.ThreadLockRecord.Get(f.ctx, thread.PostId)
				lr.AppealPending = true
				_ = f.keeper.ThreadLockRecord.Set(f.ctx, thread.PostId, lr)
			},
			expectError: true,
			errContains: "appeal already filed",
		},
		{
			name: "cooldown not passed",
			msg: &types.MsgAppealThreadLock{
				Creator: testCreator,
				RootId:  thread.PostId,
			},
			setup: func() {
				lr, _ := f.keeper.ThreadLockRecord.Get(f.ctx, thread.PostId)
				lr.LockedAt = f.sdkCtx().BlockTime().Unix() // Just locked
				_ = f.keeper.ThreadLockRecord.Set(f.ctx, thread.PostId, lr)
			},
			expectError: true,
			errContains: "must wait until",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset state
			_ = f.keeper.Params.Set(f.ctx, types.DefaultParams())
			p, _ := f.keeper.Post.Get(f.ctx, thread.PostId)
			p.Locked = true
			p.LockedBy = testSentinel
			p.Author = testCreator
			_ = f.keeper.Post.Set(f.ctx, thread.PostId, p)

			lockRecord := types.ThreadLockRecord{
				RootId:        thread.PostId,
				Sentinel:      testSentinel,
				LockedAt:      f.sdkCtx().BlockTime().Unix() - types.DefaultLockAppealCooldown - 1,
				LockReason:    "Off-topic",
				AppealPending: false,
			}
			_ = f.keeper.ThreadLockRecord.Set(f.ctx, thread.PostId, lockRecord)

			if tt.setup != nil {
				tt.setup()
			}

			resp, err := f.msgServer.AppealThreadLock(f.ctx, tt.msg)

			if tt.expectError {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.errContains)
			} else {
				require.NoError(t, err)
				require.NotNil(t, resp)

				// Verify lock record was updated
				lr, err := f.keeper.ThreadLockRecord.Get(f.ctx, thread.PostId)
				require.NoError(t, err)
				require.True(t, lr.AppealPending)
				require.NotZero(t, lr.InitiativeId)
			}
		})
	}
}

func TestAppealThreadLockNoLockRecord(t *testing.T) {
	f := initFixture(t)

	// Create a category and locked thread (locked by gov, no lock record)
	cat := f.createTestCategory(t, "General")
	thread := f.createTestPost(t, testCreator, 0, cat.CategoryId)

	// Lock the thread but don't create lock record (simulating gov lock)
	p, _ := f.keeper.Post.Get(f.ctx, thread.PostId)
	p.Locked = true
	_ = f.keeper.Post.Set(f.ctx, thread.PostId, p)

	// Attempt appeal should fail because no lock record exists
	_, err := f.msgServer.AppealThreadLock(f.ctx, &types.MsgAppealThreadLock{
		Creator: testCreator,
		RootId:  thread.PostId,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "governance authority")
}

func TestAppealThreadLockWithFee(t *testing.T) {
	f := initFixture(t)

	// Track if bank keeper was called
	bankCalled := false
	f.bankKeeper.SendCoinsFromAccountToModuleFn = func(ctx context.Context, senderAddr sdk.AccAddress, recipientModule string, amt sdk.Coins) error {
		bankCalled = true
		return nil
	}

	// Create a category and locked thread
	cat := f.createTestCategory(t, "General")
	thread := f.createTestPost(t, testCreator, 0, cat.CategoryId)

	// Lock the thread and create lock record
	p, _ := f.keeper.Post.Get(f.ctx, thread.PostId)
	p.Locked = true
	_ = f.keeper.Post.Set(f.ctx, thread.PostId, p)

	lockRecord := types.ThreadLockRecord{
		RootId:   thread.PostId,
		Sentinel: testSentinel,
		LockedAt: f.sdkCtx().BlockTime().Unix() - types.DefaultLockAppealCooldown - 1,
	}
	_ = f.keeper.ThreadLockRecord.Set(f.ctx, thread.PostId, lockRecord)

	// File appeal
	_, err := f.msgServer.AppealThreadLock(f.ctx, &types.MsgAppealThreadLock{
		Creator: testCreator,
		RootId:  thread.PostId,
	})
	require.NoError(t, err)

	// Verify bank was called for appeal fee
	require.True(t, bankCalled, "bank keeper should have been called for lock appeal fee")
}

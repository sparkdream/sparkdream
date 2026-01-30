package keeper_test

import (
	"context"
	"testing"

	"sparkdream/x/forum/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

func TestAppealThreadMove(t *testing.T) {
	f := initFixture(t)

	// Create categories and a thread that was moved
	cat1 := f.createTestCategory(t, "General")
	cat2 := f.createTestCategory(t, "Off-Topic")
	thread := f.createTestPost(t, testCreator, 0, cat2.CategoryId) // Thread is now in cat2

	// Create move record (sentinel move)
	moveRecord := types.ThreadMoveRecord{
		RootId:             thread.PostId,
		Sentinel:           testSentinel,
		OriginalCategoryId: cat1.CategoryId,
		NewCategoryId:      cat2.CategoryId,
		MovedAt:            f.sdkCtx().BlockTime().Unix() - types.DefaultMoveAppealCooldown - 1, // Past cooldown
		MoveReason:         "Better fit",
		AppealPending:      false,
	}
	_ = f.keeper.ThreadMoveRecord.Set(f.ctx, thread.PostId, moveRecord)

	tests := []struct {
		name        string
		msg         *types.MsgAppealThreadMove
		setup       func()
		expectError bool
		errContains string
	}{
		{
			name: "successful appeal",
			msg: &types.MsgAppealThreadMove{
				Creator: testCreator,
				RootId:  thread.PostId,
			},
			expectError: false,
		},
		{
			name: "invalid creator address",
			msg: &types.MsgAppealThreadMove{
				Creator: "invalid-address",
				RootId:  thread.PostId,
			},
			expectError: true,
			errContains: "invalid creator address",
		},
		{
			name: "appeals paused",
			msg: &types.MsgAppealThreadMove{
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
			msg: &types.MsgAppealThreadMove{
				Creator: testCreator,
				RootId:  9999,
			},
			expectError: true,
			errContains: "not found",
		},
		{
			name: "not thread author",
			msg: &types.MsgAppealThreadMove{
				Creator: testCreator2,
				RootId:  thread.PostId,
			},
			expectError: true,
			errContains: "only the thread author",
		},
		{
			name: "appeal already pending",
			msg: &types.MsgAppealThreadMove{
				Creator: testCreator,
				RootId:  thread.PostId,
			},
			setup: func() {
				mr, _ := f.keeper.ThreadMoveRecord.Get(f.ctx, thread.PostId)
				mr.AppealPending = true
				_ = f.keeper.ThreadMoveRecord.Set(f.ctx, thread.PostId, mr)
			},
			expectError: true,
			errContains: "appeal already filed",
		},
		{
			name: "cooldown not passed",
			msg: &types.MsgAppealThreadMove{
				Creator: testCreator,
				RootId:  thread.PostId,
			},
			setup: func() {
				mr, _ := f.keeper.ThreadMoveRecord.Get(f.ctx, thread.PostId)
				mr.MovedAt = f.sdkCtx().BlockTime().Unix() // Just moved
				_ = f.keeper.ThreadMoveRecord.Set(f.ctx, thread.PostId, mr)
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
			p.Author = testCreator
			_ = f.keeper.Post.Set(f.ctx, thread.PostId, p)

			moveRecord := types.ThreadMoveRecord{
				RootId:             thread.PostId,
				Sentinel:           testSentinel,
				OriginalCategoryId: cat1.CategoryId,
				NewCategoryId:      cat2.CategoryId,
				MovedAt:            f.sdkCtx().BlockTime().Unix() - types.DefaultMoveAppealCooldown - 1,
				MoveReason:         "Better fit",
				AppealPending:      false,
			}
			_ = f.keeper.ThreadMoveRecord.Set(f.ctx, thread.PostId, moveRecord)

			if tt.setup != nil {
				tt.setup()
			}

			resp, err := f.msgServer.AppealThreadMove(f.ctx, tt.msg)

			if tt.expectError {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.errContains)
			} else {
				require.NoError(t, err)
				require.NotNil(t, resp)

				// Verify move record was updated
				mr, err := f.keeper.ThreadMoveRecord.Get(f.ctx, thread.PostId)
				require.NoError(t, err)
				require.True(t, mr.AppealPending)
				require.NotZero(t, mr.InitiativeId)
			}
		})
	}
}

func TestAppealThreadMoveNoMoveRecord(t *testing.T) {
	f := initFixture(t)

	// Create categories and a thread
	cat1 := f.createTestCategory(t, "General")
	thread := f.createTestPost(t, testCreator, 0, cat1.CategoryId)

	// Don't create move record (no move occurred or gov move)

	// Attempt appeal should fail because no move record exists
	_, err := f.msgServer.AppealThreadMove(f.ctx, &types.MsgAppealThreadMove{
		Creator: testCreator,
		RootId:  thread.PostId,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "governance authority")
}

func TestAppealThreadMoveWithFee(t *testing.T) {
	f := initFixture(t)

	// Track if bank keeper was called
	bankCalled := false
	f.bankKeeper.SendCoinsFromAccountToModuleFn = func(ctx context.Context, senderAddr sdk.AccAddress, recipientModule string, amt sdk.Coins) error {
		bankCalled = true
		return nil
	}

	// Create categories and a thread
	cat1 := f.createTestCategory(t, "General")
	cat2 := f.createTestCategory(t, "Off-Topic")
	thread := f.createTestPost(t, testCreator, 0, cat2.CategoryId)

	// Create move record
	moveRecord := types.ThreadMoveRecord{
		RootId:             thread.PostId,
		Sentinel:           testSentinel,
		OriginalCategoryId: cat1.CategoryId,
		NewCategoryId:      cat2.CategoryId,
		MovedAt:            f.sdkCtx().BlockTime().Unix() - types.DefaultMoveAppealCooldown - 1,
	}
	_ = f.keeper.ThreadMoveRecord.Set(f.ctx, thread.PostId, moveRecord)

	// File appeal
	_, err := f.msgServer.AppealThreadMove(f.ctx, &types.MsgAppealThreadMove{
		Creator: testCreator,
		RootId:  thread.PostId,
	})
	require.NoError(t, err)

	// Verify bank was called for appeal fee
	require.True(t, bankCalled, "bank keeper should have been called for move appeal fee")
}

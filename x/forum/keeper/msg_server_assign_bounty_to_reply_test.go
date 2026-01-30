package keeper_test

import (
	"context"
	"testing"

	"sparkdream/x/forum/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

func TestAssignBountyToReply(t *testing.T) {
	f := initFixture(t)

	// Create a category, thread, and reply
	cat := f.createTestCategory(t, "General")
	thread := f.createTestPost(t, testCreator, 0, cat.CategoryId)
	reply := f.createTestPost(t, testCreator2, thread.PostId, cat.CategoryId)

	// Ensure reply has correct RootId (should be thread's PostId)
	replyPost, _ := f.keeper.Post.Get(f.ctx, reply.PostId)
	replyPost.RootId = thread.PostId
	_ = f.keeper.Post.Set(f.ctx, reply.PostId, replyPost)

	// Create an active bounty
	bounty := f.createTestBounty(t, testCreator, thread.PostId, "100000000")

	tests := []struct {
		name        string
		msg         *types.MsgAssignBountyToReply
		setup       func()
		expectError bool
		errContains string
	}{
		{
			name: "successful bounty assignment",
			msg: &types.MsgAssignBountyToReply{
				Creator:  testCreator,
				ThreadId: thread.PostId,
				ReplyId:  reply.PostId,
				Reason:   "Great answer!",
			},
			expectError: false,
		},
		{
			name: "invalid creator address",
			msg: &types.MsgAssignBountyToReply{
				Creator:  "invalid-address",
				ThreadId: thread.PostId,
				ReplyId:  reply.PostId,
				Reason:   "Test",
			},
			expectError: true,
			errContains: "invalid creator address",
		},
		{
			name: "no active bounty",
			msg: &types.MsgAssignBountyToReply{
				Creator:  testCreator,
				ThreadId: 9999,
				ReplyId:  reply.PostId,
				Reason:   "Test",
			},
			expectError: true,
			errContains: "no active bounty",
		},
		{
			name: "not bounty creator",
			msg: &types.MsgAssignBountyToReply{
				Creator:  testCreator2,
				ThreadId: thread.PostId,
				ReplyId:  reply.PostId,
				Reason:   "Test",
			},
			expectError: true,
			errContains: "only the bounty creator",
		},
		{
			name: "reply not found",
			msg: &types.MsgAssignBountyToReply{
				Creator:  testCreator,
				ThreadId: thread.PostId,
				ReplyId:  9999,
				Reason:   "Test",
			},
			expectError: true,
			errContains: "not found",
		},
		{
			name: "reply not in thread",
			msg: &types.MsgAssignBountyToReply{
				Creator:  testCreator,
				ThreadId: thread.PostId,
				ReplyId:  thread.PostId, // Root post, not a reply
				Reason:   "Test",
			},
			expectError: true,
			errContains: "cannot award bounty to thread root",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset bounty state
			b, _ := f.keeper.Bounty.Get(f.ctx, bounty.Id)
			b.Status = types.BountyStatus_BOUNTY_STATUS_ACTIVE
			b.Awards = nil
			b.ExpiresAt = f.sdkCtx().BlockTime().Unix() + 86400*7
			_ = f.keeper.Bounty.Set(f.ctx, bounty.Id, b)

			if tt.setup != nil {
				tt.setup()
			}

			resp, err := f.msgServer.AssignBountyToReply(f.ctx, tt.msg)

			if tt.expectError {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.errContains)
			} else {
				require.NoError(t, err)
				require.NotNil(t, resp)

				// Verify bounty was awarded
				b, err := f.keeper.Bounty.Get(f.ctx, bounty.Id)
				require.NoError(t, err)
				require.Equal(t, types.BountyStatus_BOUNTY_STATUS_AWARDED, b.Status)
				require.Len(t, b.Awards, 1)
				require.Equal(t, reply.PostId, b.Awards[0].PostId)
				require.Equal(t, testCreator2, b.Awards[0].Recipient)
			}
		})
	}
}

func TestAssignBountyTransfer(t *testing.T) {
	f := initFixture(t)

	// Track bank calls
	var transferredTo sdk.AccAddress
	var transferredAmount sdk.Coins
	f.bankKeeper.SendCoinsFromModuleToAccountFn = func(ctx context.Context, senderModule string, recipientAddr sdk.AccAddress, amt sdk.Coins) error {
		transferredTo = recipientAddr
		transferredAmount = amt
		return nil
	}

	// Create a category, thread, and reply
	cat := f.createTestCategory(t, "General")
	thread := f.createTestPost(t, testCreator, 0, cat.CategoryId)
	reply := f.createTestPost(t, testCreator2, thread.PostId, cat.CategoryId)

	// Ensure reply has correct RootId
	replyPost, _ := f.keeper.Post.Get(f.ctx, reply.PostId)
	replyPost.RootId = thread.PostId
	_ = f.keeper.Post.Set(f.ctx, reply.PostId, replyPost)

	// Create an active bounty
	bounty := f.createTestBounty(t, testCreator, thread.PostId, "100000000")

	// Assign bounty
	_, err := f.msgServer.AssignBountyToReply(f.ctx, &types.MsgAssignBountyToReply{
		Creator:  testCreator,
		ThreadId: thread.PostId,
		ReplyId:  reply.PostId,
		Reason:   "Great answer!",
	})
	require.NoError(t, err)

	// Verify transfer happened
	require.NotNil(t, transferredTo)
	require.Equal(t, bounty.Amount, transferredAmount.AmountOf(types.DefaultFeeDenom).String())
}

func TestAssignBountyExpired(t *testing.T) {
	f := initFixture(t)

	// Create a category, thread, and reply
	cat := f.createTestCategory(t, "General")
	thread := f.createTestPost(t, testCreator, 0, cat.CategoryId)
	reply := f.createTestPost(t, testCreator2, thread.PostId, cat.CategoryId)

	// Ensure reply has correct RootId
	replyPost, _ := f.keeper.Post.Get(f.ctx, reply.PostId)
	replyPost.RootId = thread.PostId
	_ = f.keeper.Post.Set(f.ctx, reply.PostId, replyPost)

	// Create an expired bounty
	bounty := f.createTestBounty(t, testCreator, thread.PostId, "100000000")
	b, _ := f.keeper.Bounty.Get(f.ctx, bounty.Id)
	b.ExpiresAt = f.sdkCtx().BlockTime().Unix() - 1 // Expired
	_ = f.keeper.Bounty.Set(f.ctx, bounty.Id, b)

	// Try to assign
	_, err := f.msgServer.AssignBountyToReply(f.ctx, &types.MsgAssignBountyToReply{
		Creator:  testCreator,
		ThreadId: thread.PostId,
		ReplyId:  reply.PostId,
		Reason:   "Test",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "expired")
}

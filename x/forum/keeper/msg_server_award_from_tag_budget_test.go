package keeper_test

import (
	"context"
	"testing"

	"sparkdream/x/forum/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

func TestAwardFromTagBudget(t *testing.T) {
	f := initFixture(t)

	// Create a category, tag, and post with that tag
	cat := f.createTestCategory(t, "General")
	f.createTestTag(t, "golang")

	post := f.createTestPost(t, testCreator, 0, cat.CategoryId)
	p, _ := f.keeper.Post.Get(f.ctx, post.PostId)
	p.Tags = []string{"golang"}
	_ = f.keeper.Post.Set(f.ctx, post.PostId, p)

	// Get authority address (which acts as a group account in tests)
	groupAccount, _ := f.addressCodec.BytesToString(f.keeper.GetAuthority())

	// Create a tag budget
	budget := f.createTestTagBudget(t, groupAccount, "golang", "1000000000")

	tests := []struct {
		name        string
		msg         *types.MsgAwardFromTagBudget
		setup       func()
		expectError bool
		errContains string
	}{
		{
			name: "successful award",
			msg: &types.MsgAwardFromTagBudget{
				Creator:  groupAccount,
				BudgetId: budget.Id,
				PostId:   post.PostId,
				Amount:   "100000000", // 100 SPARK
				Reason:   "Great post!",
			},
			expectError: false,
		},
		{
			name: "invalid creator address",
			msg: &types.MsgAwardFromTagBudget{
				Creator:  "invalid-address",
				BudgetId: budget.Id,
				PostId:   post.PostId,
				Amount:   "100000000",
				Reason:   "Test",
			},
			expectError: true,
			errContains: "invalid creator address",
		},
		{
			name: "budget not found",
			msg: &types.MsgAwardFromTagBudget{
				Creator:  groupAccount,
				BudgetId: 9999,
				PostId:   post.PostId,
				Amount:   "100000000",
				Reason:   "Test",
			},
			expectError: true,
			errContains: "budget not found",
		},
		{
			name: "budget not active",
			msg: &types.MsgAwardFromTagBudget{
				Creator:  groupAccount,
				BudgetId: budget.Id,
				PostId:   post.PostId,
				Amount:   "100000000",
				Reason:   "Test",
			},
			setup: func() {
				b, _ := f.keeper.TagBudget.Get(f.ctx, budget.Id)
				b.Active = false
				_ = f.keeper.TagBudget.Set(f.ctx, budget.Id, b)
			},
			expectError: true,
			errContains: "not active",
		},
		// NOTE: "not group member" test skipped - group membership verification is stubbed
		// The implementation currently allows any address to award from budgets
		// This will be enforced when x/commons integration is complete
		{
			name: "post not found",
			msg: &types.MsgAwardFromTagBudget{
				Creator:  groupAccount,
				BudgetId: budget.Id,
				PostId:   9999,
				Amount:   "100000000",
				Reason:   "Test",
			},
			expectError: true,
			errContains: "post not found",
		},
		{
			name: "post missing tag",
			msg: &types.MsgAwardFromTagBudget{
				Creator:  groupAccount,
				BudgetId: budget.Id,
				PostId:   post.PostId,
				Amount:   "100000000",
				Reason:   "Test",
			},
			setup: func() {
				// Remove the tag from the post
				p, _ := f.keeper.Post.Get(f.ctx, post.PostId)
				p.Tags = []string{}
				_ = f.keeper.Post.Set(f.ctx, post.PostId, p)
			},
			expectError: true,
			errContains: "does not have tag",
		},
		{
			name: "invalid amount",
			msg: &types.MsgAwardFromTagBudget{
				Creator:  groupAccount,
				BudgetId: budget.Id,
				PostId:   post.PostId,
				Amount:   "invalid",
				Reason:   "Test",
			},
			expectError: true,
			errContains: "invalid",
		},
		{
			name: "amount exceeds pool",
			msg: &types.MsgAwardFromTagBudget{
				Creator:  groupAccount,
				BudgetId: budget.Id,
				PostId:   post.PostId,
				Amount:   "9999999999999", // More than pool balance
				Reason:   "Test",
			},
			expectError: true,
			errContains: "exceeds pool balance",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset budget state
			b, _ := f.keeper.TagBudget.Get(f.ctx, budget.Id)
			b.Active = true
			b.PoolBalance = "1000000000"
			_ = f.keeper.TagBudget.Set(f.ctx, budget.Id, b)

			// Reset post tags
			p, _ := f.keeper.Post.Get(f.ctx, post.PostId)
			p.Tags = []string{"golang"}
			_ = f.keeper.Post.Set(f.ctx, post.PostId, p)

			if tt.setup != nil {
				tt.setup()
			}

			resp, err := f.msgServer.AwardFromTagBudget(f.ctx, tt.msg)

			if tt.expectError {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.errContains)
			} else {
				require.NoError(t, err)
				require.NotNil(t, resp)

				// Verify budget balance was reduced
				b, err := f.keeper.TagBudget.Get(f.ctx, budget.Id)
				require.NoError(t, err)
				require.Equal(t, "900000000", b.PoolBalance) // 1000 - 100 = 900

				// Verify award record was created
				var found bool
				iter, _ := f.keeper.TagBudgetAward.Iterate(f.ctx, nil)
				for ; iter.Valid(); iter.Next() {
					award, _ := iter.Value()
					if award.BudgetId == budget.Id && award.PostId == post.PostId {
						found = true
						require.Equal(t, tt.msg.Amount, award.Amount)
						require.Equal(t, tt.msg.Reason, award.Reason)
						require.Equal(t, testCreator, award.Recipient) // Post author
						break
					}
				}
				iter.Close()
				require.True(t, found, "award record should have been created")
			}
		})
	}
}

func TestAwardFromTagBudgetTransfer(t *testing.T) {
	f := initFixture(t)

	// Track bank calls
	var transferredTo sdk.AccAddress
	var transferredAmount sdk.Coins
	f.bankKeeper.SendCoinsFromModuleToAccountFn = func(ctx context.Context, senderModule string, recipientAddr sdk.AccAddress, amt sdk.Coins) error {
		transferredTo = recipientAddr
		transferredAmount = amt
		return nil
	}

	// Create a category, tag, and post
	cat := f.createTestCategory(t, "General")
	f.createTestTag(t, "golang")

	post := f.createTestPost(t, testCreator, 0, cat.CategoryId)
	p, _ := f.keeper.Post.Get(f.ctx, post.PostId)
	p.Tags = []string{"golang"}
	_ = f.keeper.Post.Set(f.ctx, post.PostId, p)

	// Get authority address
	groupAccount, _ := f.addressCodec.BytesToString(f.keeper.GetAuthority())

	// Create a tag budget
	budget := f.createTestTagBudget(t, groupAccount, "golang", "1000000000")

	// Award from budget
	_, err := f.msgServer.AwardFromTagBudget(f.ctx, &types.MsgAwardFromTagBudget{
		Creator:  groupAccount,
		BudgetId: budget.Id,
		PostId:   post.PostId,
		Amount:   "100000000",
		Reason:   "Great post!",
	})
	require.NoError(t, err)

	// Verify transfer happened
	require.NotNil(t, transferredTo)
	require.Equal(t, "100000000", transferredAmount.AmountOf(types.DefaultFeeDenom).String())
}

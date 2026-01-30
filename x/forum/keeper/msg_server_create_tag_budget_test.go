package keeper_test

import (
	"context"
	"testing"

	"sparkdream/x/forum/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

func TestCreateTagBudget(t *testing.T) {
	f := initFixture(t)

	// Create a tag
	f.createTestTag(t, "golang")

	// Get authority address (which acts as a group account in tests)
	groupAccount, _ := f.addressCodec.BytesToString(f.keeper.GetAuthority())

	tests := []struct {
		name        string
		msg         *types.MsgCreateTagBudget
		setup       func()
		expectError bool
		errContains string
	}{
		{
			name: "successful tag budget creation",
			msg: &types.MsgCreateTagBudget{
				Creator:     groupAccount,
				Tag:         "golang",
				InitialPool: "1000000000", // 1000 SPARK
				MembersOnly: false,
			},
			expectError: false,
		},
		{
			name: "invalid creator address",
			msg: &types.MsgCreateTagBudget{
				Creator:     "invalid-address",
				Tag:         "golang",
				InitialPool: "1000000000",
				MembersOnly: false,
			},
			expectError: true,
			errContains: "invalid creator address",
		},
		// NOTE: "not a group account" test skipped - group account verification is stubbed
		// The implementation currently allows any address to create tag budgets
		// This will be enforced when x/commons integration is complete
		{
			name: "tag not found",
			msg: &types.MsgCreateTagBudget{
				Creator:     groupAccount,
				Tag:         "nonexistent",
				InitialPool: "1000000000",
				MembersOnly: false,
			},
			expectError: true,
			errContains: "tag not found",
		},
		{
			name: "invalid initial pool amount",
			msg: &types.MsgCreateTagBudget{
				Creator:     groupAccount,
				Tag:         "golang",
				InitialPool: "invalid",
				MembersOnly: false,
			},
			expectError: true,
			errContains: "invalid",
		},
		{
			name: "zero initial pool",
			msg: &types.MsgCreateTagBudget{
				Creator:     groupAccount,
				Tag:         "golang",
				InitialPool: "0",
				MembersOnly: false,
			},
			expectError: true,
			errContains: "invalid",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clean up any existing tag budgets
			iter, _ := f.keeper.TagBudget.Iterate(f.ctx, nil)
			for ; iter.Valid(); iter.Next() {
				budget, _ := iter.Value()
				_ = f.keeper.TagBudget.Remove(f.ctx, budget.Id)
			}
			iter.Close()

			if tt.setup != nil {
				tt.setup()
			}

			resp, err := f.msgServer.CreateTagBudget(f.ctx, tt.msg)

			if tt.expectError {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.errContains)
			} else {
				require.NoError(t, err)
				require.NotNil(t, resp)

				// Find the created budget
				var found bool
				iter, _ := f.keeper.TagBudget.Iterate(f.ctx, nil)
				for ; iter.Valid(); iter.Next() {
					budget, _ := iter.Value()
					if budget.Tag == tt.msg.Tag && budget.GroupAccount == tt.msg.Creator {
						found = true
						require.Equal(t, tt.msg.InitialPool, budget.PoolBalance)
						require.Equal(t, tt.msg.MembersOnly, budget.MembersOnly)
						require.True(t, budget.Active)
						break
					}
				}
				iter.Close()
				require.True(t, found, "budget should have been created")
			}
		})
	}
}

func TestCreateTagBudgetEscrow(t *testing.T) {
	f := initFixture(t)

	// Track bank calls
	var escrowedAmount sdk.Coins
	f.bankKeeper.SendCoinsFromAccountToModuleFn = func(ctx context.Context, senderAddr sdk.AccAddress, recipientModule string, amt sdk.Coins) error {
		escrowedAmount = amt
		return nil
	}

	// Create a tag
	f.createTestTag(t, "golang")

	// Get authority address
	groupAccount, _ := f.addressCodec.BytesToString(f.keeper.GetAuthority())

	// Create tag budget
	_, err := f.msgServer.CreateTagBudget(f.ctx, &types.MsgCreateTagBudget{
		Creator:     groupAccount,
		Tag:         "golang",
		InitialPool: "1000000000",
		MembersOnly: false,
	})
	require.NoError(t, err)

	// Verify funds were escrowed
	require.NotEmpty(t, escrowedAmount)
	require.Equal(t, "1000000000", escrowedAmount.AmountOf(types.DefaultFeeDenom).String())
}

func TestCreateTagBudgetDuplicate(t *testing.T) {
	f := initFixture(t)

	// Create a tag
	f.createTestTag(t, "golang")

	// Get authority address
	groupAccount, _ := f.addressCodec.BytesToString(f.keeper.GetAuthority())

	// Create first tag budget
	_, err := f.msgServer.CreateTagBudget(f.ctx, &types.MsgCreateTagBudget{
		Creator:     groupAccount,
		Tag:         "golang",
		InitialPool: "1000000000",
		MembersOnly: false,
	})
	require.NoError(t, err)

	// Try to create another budget for the same tag from the same group
	_, err = f.msgServer.CreateTagBudget(f.ctx, &types.MsgCreateTagBudget{
		Creator:     groupAccount,
		Tag:         "golang",
		InitialPool: "500000000",
		MembersOnly: false,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "already exists")
}

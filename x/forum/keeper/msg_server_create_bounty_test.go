package keeper_test

import (
	"context"
	"testing"

	"sparkdream/x/forum/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

func TestCreateBounty(t *testing.T) {
	f := initFixture(t)

	// Create a category and thread
	cat := f.createTestCategory(t, "General")
	thread := f.createTestPost(t, testCreator, 0, cat.CategoryId)

	tests := []struct {
		name        string
		msg         *types.MsgCreateBounty
		setup       func()
		expectError bool
		errContains string
	}{
		{
			name: "successful bounty creation",
			msg: &types.MsgCreateBounty{
				Creator:  testCreator,
				ThreadId: thread.PostId,
				Amount:   "100000000", // 100 SPARK
			},
			expectError: false,
		},
		{
			name: "invalid creator address",
			msg: &types.MsgCreateBounty{
				Creator:  "invalid-address",
				ThreadId: thread.PostId,
				Amount:   "100000000",
			},
			expectError: true,
			errContains: "invalid creator address",
		},
		{
			name: "bounties disabled",
			msg: &types.MsgCreateBounty{
				Creator:  testCreator,
				ThreadId: thread.PostId,
				Amount:   "100000000",
			},
			setup: func() {
				params := types.DefaultParams()
				params.BountiesEnabled = false
				_ = f.keeper.Params.Set(f.ctx, params)
			},
			expectError: true,
			errContains: "bounties are disabled",
		},
		{
			name: "thread not found",
			msg: &types.MsgCreateBounty{
				Creator:  testCreator,
				ThreadId: 9999,
				Amount:   "100000000",
			},
			expectError: true,
			errContains: "not found",
		},
		{
			name: "not thread author",
			msg: &types.MsgCreateBounty{
				Creator:  testCreator2,
				ThreadId: thread.PostId,
				Amount:   "100000000",
			},
			expectError: true,
			errContains: "only the thread author",
		},
		{
			name: "invalid amount",
			msg: &types.MsgCreateBounty{
				Creator:  testCreator,
				ThreadId: thread.PostId,
				Amount:   "invalid",
			},
			expectError: true,
			errContains: "invalid amount",
		},
		{
			name: "amount below minimum",
			msg: &types.MsgCreateBounty{
				Creator:  testCreator,
				ThreadId: thread.PostId,
				Amount:   "10", // Below minimum (min is 50)
			},
			expectError: true,
			errContains: "minimum bounty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset params
			_ = f.keeper.Params.Set(f.ctx, types.DefaultParams())

			// Remove any existing bounty for the thread to allow re-testing
			iter, _ := f.keeper.Bounty.Iterate(f.ctx, nil)
			for ; iter.Valid(); iter.Next() {
				b, _ := iter.Value()
				if b.ThreadId == thread.PostId {
					_ = f.keeper.Bounty.Remove(f.ctx, b.Id)
				}
			}
			iter.Close()

			if tt.setup != nil {
				tt.setup()
			}

			resp, err := f.msgServer.CreateBounty(f.ctx, tt.msg)

			if tt.expectError {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.errContains)
			} else {
				require.NoError(t, err)
				require.NotNil(t, resp)
			}
		})
	}
}

func TestCreateBountyEscrow(t *testing.T) {
	f := initFixture(t)

	// Track bank calls
	var escrowedAmount sdk.Coins
	f.bankKeeper.SendCoinsFromAccountToModuleFn = func(ctx context.Context, senderAddr sdk.AccAddress, recipientModule string, amt sdk.Coins) error {
		escrowedAmount = amt
		return nil
	}

	// Create a category and thread
	cat := f.createTestCategory(t, "General")
	thread := f.createTestPost(t, testCreator, 0, cat.CategoryId)

	// Create bounty
	_, err := f.msgServer.CreateBounty(f.ctx, &types.MsgCreateBounty{
		Creator:  testCreator,
		ThreadId: thread.PostId,
		Amount:   "100000000",
	})
	require.NoError(t, err)

	// Verify funds were escrowed
	require.NotEmpty(t, escrowedAmount)
	require.Equal(t, "100000000", escrowedAmount.AmountOf(types.DefaultFeeDenom).String())
}

package keeper_test

import (
	"context"
	"testing"

	"sparkdream/testutil"
	"sparkdream/x/futarchy/keeper"
	"sparkdream/x/futarchy/types"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

func TestMsgTrade(t *testing.T) {
	// Test Addresses
	alice := sdk.AccAddress([]byte("alice"))

	tests := []struct {
		name       string
		market     types.Market // Market state to setup
		msg        types.MsgTrade
		expectErr  bool
		errMsg     string
		checkState func(t *testing.T, m *MockBankKeeper, ctx context.Context, res *types.MsgTradeResponse)
	}{
		{
			name: "Success - Buy YES",
			market: types.Market{
				Index:  1,
				Status: "ACTIVE",
				// b = 1000 / ln(2) ≈ 1442.69
				BValue:             testutil.DecPtr("1442.695040888963407360"),
				PoolYes:            testutil.IntPtr(0),
				PoolNo:             testutil.IntPtr(0),
				MinTick:            testutil.IntPtr(10),
				Denom:              "uspark",
				Creator:            "creator",
				EndBlock:           1000,
				InitialLiquidity:   testutil.IntPtr(1000),
				LiquidityWithdrawn: testutil.IntPtr(0),
			},
			msg: types.MsgTrade{
				Creator:  alice.String(),
				MarketId: 1,
				AmountIn: testutil.IntPtr(1000), // Just the amount, Denom comes from Market
				IsYes:    true,
			},
			expectErr: false,
			checkState: func(t *testing.T, m *MockBankKeeper, ctx context.Context, res *types.MsgTradeResponse) {
				// 1. Verify Alice paid 1000 uspark
				// Initial was 1000000, should be 999000
				balance := m.GetBalance(ctx, alice, "uspark")
				require.Equal(t, math.NewInt(999000), balance.Amount, "Alice should have paid 1000 uspark")

				// 2. Verify Alice received shares
				// SharesOut from response
				sharesOutInt := *res.SharesOut

				shares := m.GetBalance(ctx, alice, "f/1/yes")
				require.Equal(t, sharesOutInt, shares.Amount, "Alice should have received correct YES shares")
				require.True(t, shares.Amount.IsPositive(), "Shares amount must be positive")
			},
		},
		{
			name: "Success - Buy NO",
			market: types.Market{
				Index:              2,
				Status:             "ACTIVE",
				BValue:             testutil.DecPtr("1442.695040888963407360"),
				PoolYes:            testutil.IntPtr(0),
				PoolNo:             testutil.IntPtr(0),
				MinTick:            testutil.IntPtr(10),
				Denom:              "uspark",
				Creator:            "creator",
				EndBlock:           1000,
				InitialLiquidity:   testutil.IntPtr(1000),
				LiquidityWithdrawn: testutil.IntPtr(0),
			},
			msg: types.MsgTrade{
				Creator:  alice.String(),
				MarketId: 2,
				AmountIn: testutil.IntPtr(500),
				IsYes:    false,
			},
			expectErr: false,
			checkState: func(t *testing.T, m *MockBankKeeper, ctx context.Context, res *types.MsgTradeResponse) {
				// 1. Verify Alice paid 500 uspark
				balance := m.GetBalance(ctx, alice, "uspark")
				require.Equal(t, math.NewInt(999500), balance.Amount)

				// 2. Verify Alice received NO shares
				sharesOutInt := *res.SharesOut

				shares := m.GetBalance(ctx, alice, "f/2/no")
				require.Equal(t, sharesOutInt, shares.Amount)
			},
		},
		{
			name: "Error - Market Not Found",
			market: types.Market{
				Index: 3, // We won't save this one
			},
			msg: types.MsgTrade{
				Creator:  alice.String(),
				MarketId: 999, // ID doesn't exist
				AmountIn: testutil.IntPtr(1000),
				IsYes:    true,
			},
			expectErr: true,
			errMsg:    "not found",
		},
		{
			name: "Error - Trade Too Small (MinTick)",
			market: types.Market{
				Index:   4,
				Status:  "ACTIVE",
				BValue:  testutil.DecPtr("1000"),
				PoolYes: testutil.IntPtr(0),
				PoolNo:  testutil.IntPtr(0),
				// Set a high MinTick to trigger error
				MinTick:            testutil.IntPtr(1000000),
				Denom:              "uspark",
				InitialLiquidity:   testutil.IntPtr(1000),
				LiquidityWithdrawn: testutil.IntPtr(0),
			},
			msg: types.MsgTrade{
				Creator:  alice.String(),
				MarketId: 4,
				AmountIn: testutil.IntPtr(10), // 10 < 1000000
				IsYes:    true,
			},
			expectErr: true,
			errMsg:    "Trade too small",
		},
		{
			name: "Error - Market Not Active",
			market: types.Market{
				Index:              6,
				Status:             "RESOLVED_YES",
				BValue:             testutil.DecPtr("1000"),
				PoolYes:            testutil.IntPtr(0),
				PoolNo:             testutil.IntPtr(0),
				MinTick:            testutil.IntPtr(1),
				Denom:              "uspark",
				InitialLiquidity:   testutil.IntPtr(1000),
				LiquidityWithdrawn: testutil.IntPtr(0),
			},
			msg: types.MsgTrade{
				Creator:  alice.String(),
				MarketId: 6,
				AmountIn: testutil.IntPtr(1000),
				IsYes:    true,
			},
			expectErr: true,
			errMsg:    "is not active",
		},
		{
			name: "Error - Invalid Trade Amount",
			market: types.Market{
				Index:   5,
				Status:  "ACTIVE",
				BValue:  testutil.DecPtr("1000"),
				PoolYes: testutil.IntPtr(0),
				PoolNo:  testutil.IntPtr(0),
				MinTick: testutil.IntPtr(1),
				Denom:   "uspark",
			},
			msg: types.MsgTrade{
				Creator:  alice.String(),
				MarketId: 5,
				AmountIn: nil,
				IsYes:    true,
			},
			expectErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// 1. Init Fresh Fixture per test
			f := initFixture(t)
			ms := keeper.NewMsgServerImpl(f.keeper)
			ctx := sdk.UnwrapSDKContext(f.ctx)

			// Fund Alice
			f.bankKeeper.SetBalance(alice, sdk.NewCoin("uspark", math.NewInt(1000000)))

			// 2. Setup Market State
			// Only save if the test case actually defines a valid index
			// Check against nil pointer for BValue to determine if market is "real" in this test struct
			if tc.market.Index > 0 && tc.market.BValue != nil && tc.name != "Error - Market Not Found" {
				err := f.keeper.Market.Set(ctx, tc.market.Index, tc.market)
				require.NoError(t, err)
			}

			// 3. Execute Trade
			res, err := ms.Trade(ctx, &tc.msg)

			// 4. Assertions
			if tc.expectErr {
				require.Error(t, err)
				if tc.errMsg != "" {
					require.Contains(t, err.Error(), tc.errMsg)
				}
			} else {
				require.NoError(t, err)
				require.NotNil(t, res)

				// A. Verify Shares Output
				require.True(t, res.SharesOut.IsPositive(), "shares out should be positive")

				// B. Verify Market State Update
				updatedMarket, err := f.keeper.Market.Get(ctx, tc.market.Index)
				require.NoError(t, err)

				if tc.msg.IsYes {
					// Check PoolYes Increased
					oldYes := tc.market.PoolYes
					newYes := updatedMarket.PoolYes
					require.True(t, newYes.GT(*oldYes), "PoolYes should increase")
				} else {
					// Check PoolNo Increased
					oldNo := tc.market.PoolNo
					newNo := updatedMarket.PoolNo
					require.True(t, newNo.GT(*oldNo), "PoolNo should increase")
				}

				// C. Custom State Checks (Balance verification)
				if tc.checkState != nil {
					tc.checkState(t, f.bankKeeper, f.ctx, res)
				}
			}
		})
	}
}

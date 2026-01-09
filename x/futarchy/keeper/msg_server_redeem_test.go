package keeper_test

import (
	"context"
	"testing"

	"sparkdream/x/futarchy/keeper"
	"sparkdream/x/futarchy/types"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

func TestMsgRedeem(t *testing.T) {
	// Addresses
	alice := sdk.AccAddress([]byte("alice"))

	tests := []struct {
		name        string
		msg         types.MsgRedeem
		blockHeight int64
		setupBal    func(m *MockBankKeeper)
		expectErr   bool
		errMsg      string
		checkState  func(t *testing.T, m *MockBankKeeper, ctx context.Context)
	}{
		{
			name:        "Success - Standard Redemption (YES)",
			msg:         types.MsgRedeem{Creator: alice.String(), MarketId: 1},
			blockHeight: 110,
			setupBal: func(m *MockBankKeeper) {
				// Give Alice winning shares: f/1/yes
				m.SetBalance(alice, sdk.NewCoin("f/1/yes", math.NewInt(100)))
			},
			expectErr: false,
			checkState: func(t *testing.T, m *MockBankKeeper, ctx context.Context) {
				// Shares should be gone
				shares := m.GetBalance(ctx, alice, "f/1/yes")
				require.True(t, shares.Amount.IsZero(), "expected 0 shares left, got %s", shares)

				// Should have received 100 uspark
				collateral := m.GetBalance(ctx, alice, "uspark")
				require.Equal(t, math.NewInt(100), collateral.Amount, "expected 100 uspark")
			},
		},
		{
			name:        "Error - No Winning Shares",
			msg:         types.MsgRedeem{Creator: alice.String(), MarketId: 1},
			blockHeight: 110,
			setupBal: func(m *MockBankKeeper) {
				// Alice has LOSING shares: f/1/no (Balance of yes is 0)
				m.SetBalance(alice, sdk.NewCoin("f/1/no", math.NewInt(100)))
			},
			expectErr: true,
			errMsg:    "you have no winning shares",
		},
		{
			name:        "Error - Market Active",
			msg:         types.MsgRedeem{Creator: alice.String(), MarketId: 3},
			blockHeight: 110,
			setupBal:    func(m *MockBankKeeper) {},
			expectErr:   true,
			errMsg:      "market is not resolved yet",
		},
		{
			name:        "Error - Market Not Found",
			msg:         types.MsgRedeem{Creator: alice.String(), MarketId: 999},
			blockHeight: 110,
			setupBal:    func(m *MockBankKeeper) {},
			expectErr:   true,
			errMsg:      "not found",
		},
		{
			name: "Success - Delayed Redemption (Time Passed)",
			msg:  types.MsgRedeem{Creator: alice.String(), MarketId: 2},
			// ResHeight(100) + Delay(50) = 150. Current is 151.
			blockHeight: 151,
			setupBal: func(m *MockBankKeeper) {
				// Winner is NO. Give Alice f/2/no
				m.SetBalance(alice, sdk.NewCoin("f/2/no", math.NewInt(50)))
			},
			expectErr: false,
			checkState: func(t *testing.T, m *MockBankKeeper, ctx context.Context) {
				// Shares should be gone
				shares := m.GetBalance(ctx, alice, "f/2/no")
				require.True(t, shares.Amount.IsZero(), "expected 0 shares left, got %s", shares)

				// Should have received 50 uspark
				collateral := m.GetBalance(ctx, alice, "uspark")
				require.Equal(t, math.NewInt(50), collateral.Amount, "expected 50 uspark")
			},
		},
		{
			name: "Error - Early Redemption (Locked)",
			msg:  types.MsgRedeem{Creator: alice.String(), MarketId: 2},
			// ResHeight(100) + Delay(50) = 150. Current is 149.
			blockHeight: 149,
			setupBal: func(m *MockBankKeeper) {
				m.SetBalance(alice, sdk.NewCoin("f/2/no", math.NewInt(50)))
			},
			expectErr: true,
			errMsg:    "redemption locked until block 150",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// 1. Initialize Fixture PER TEST (Fresh State)
			f := initFixture(t)
			ms := keeper.NewMsgServerImpl(f.keeper)
			ctx := sdk.UnwrapSDKContext(f.ctx)
			mockBank := f.bankKeeper

			// 2. Setup Markets (Must be re-done because store is fresh)
			// Market 1: Resolved YES, No Delay
			m1 := types.Market{
				Index:            1,
				Status:           "RESOLVED_YES",
				Denom:            "uspark",
				RedemptionBlocks: 0,
				ResolutionHeight: 100,
			}
			_ = f.keeper.Market.Set(ctx, 1, m1)

			// Market 2: Resolved NO, With Delay
			m2 := types.Market{
				Index:            2,
				Status:           "RESOLVED_NO",
				Denom:            "uspark",
				RedemptionBlocks: 50,
				ResolutionHeight: 100,
			}
			_ = f.keeper.Market.Set(ctx, 2, m2)

			// Market 3: Active
			m3 := types.Market{
				Index:  3,
				Status: "ACTIVE",
				Denom:  "uspark",
			}
			_ = f.keeper.Market.Set(ctx, 3, m3)

			// 3. Set Context Height
			ctx = ctx.WithBlockHeight(tc.blockHeight)

			// 4. Setup Balances
			if tc.setupBal != nil {
				tc.setupBal(mockBank)
			}

			// 5. Execute
			_, err := ms.Redeem(ctx, &tc.msg)

			if tc.expectErr {
				require.Error(t, err)
				if tc.errMsg != "" {
					require.Contains(t, err.Error(), tc.errMsg)
				}
			} else {
				require.NoError(t, err)
				if tc.checkState != nil {
					tc.checkState(t, mockBank, f.ctx)
				}
			}
		})
	}
}

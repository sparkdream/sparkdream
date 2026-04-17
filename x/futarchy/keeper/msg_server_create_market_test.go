package keeper_test

import (
	"context"
	"fmt"
	"testing"

	"sparkdream/testutil"
	"sparkdream/x/futarchy/keeper"
	"sparkdream/x/futarchy/types"
	reptypes "sparkdream/x/rep/types"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

// stubRepKeeper implements types.RepKeeper with a configurable trust level.
type stubRepKeeper struct {
	trust   reptypes.TrustLevel
	lookup  error
	tracked map[string]reptypes.TrustLevel
}

func (s *stubRepKeeper) GetTrustLevel(_ context.Context, addr sdk.AccAddress) (reptypes.TrustLevel, error) {
	if s.lookup != nil {
		return reptypes.TrustLevel_TRUST_LEVEL_NEW, s.lookup
	}
	if lvl, ok := s.tracked[addr.String()]; ok {
		return lvl, nil
	}
	return s.trust, nil
}

func TestMsgCreateMarket(t *testing.T) {
	// Create test addresses
	alice := sdk.AccAddress([]byte("alice"))

	tests := []struct {
		name       string
		msg        types.MsgCreateMarket
		expectErr  bool
		checkState func(t *testing.T, k keeper.Keeper, m *MockBankKeeper, ctx context.Context, res *types.MsgCreateMarketResponse)
	}{
		{
			name: "Success - Governance proposal outcome market",
			msg: types.MsgCreateMarket{
				Creator:          alice.String(),
				Symbol:           "PROP-42",
				Question:         "Will governance proposal #42 pass?",
				InitialLiquidity: testutil.IntPtr(100000),
				EndBlock:         110, // Current height assumed 10 (initFixture default? We will check ctx)
			},
			expectErr: false,
			checkState: func(t *testing.T, k keeper.Keeper, m *MockBankKeeper, ctx context.Context, res *types.MsgCreateMarketResponse) {
				// 1. Verify Market Exists in Store
				market, err := k.Market.Get(ctx, res.MarketId)
				require.NoError(t, err)
				require.Equal(t, "PROP-42", market.Symbol)
				require.Equal(t, "ACTIVE", market.Status)
				require.NotNil(t, market.InitialLiquidity)
				require.True(t, math.NewInt(100000).Equal(*market.InitialLiquidity), "Initial liquidity should be 100000")

				// 2. Verify Alice's Balance Deducted
				// Alice started with 100,000 and used 100,000 for liquidity. Balance should be 0.
				balance := m.GetBalance(ctx, alice, "uspark")

				require.True(t, balance.Amount.IsZero(), "Alice's balance should be empty after creating market")
			},
		},
		{
			name: "Error - Insufficient Funds",
			msg: types.MsgCreateMarket{
				Creator:          alice.String(),
				Symbol:           "BROKE",
				Question:         "Am I broke?",
				InitialLiquidity: testutil.IntPtr(999999), // More than alice has
				EndBlock:         1000,
			},
			expectErr: true,
		},
		{
			name: "Error - Invalid Duration (EndBlock <= Current)",
			msg: types.MsgCreateMarket{
				Creator:          alice.String(),
				Symbol:           "INVALID-TIME",
				Question:         "Time travel?",
				InitialLiquidity: testutil.IntPtr(1000),
				EndBlock:         10, // Equal to current height (10)
			},
			expectErr: true,
		},
		{
			name: "Error - Past Duration",
			msg: types.MsgCreateMarket{
				Creator:          alice.String(),
				Symbol:           "INVALID-PAST",
				Question:         "Past?",
				InitialLiquidity: testutil.IntPtr(1000),
				EndBlock:         5, // 5 < 10
			},
			expectErr: true,
		},
		{
			name: "Error - Invalid Liquidity Coin (Nil)",
			msg: types.MsgCreateMarket{
				Creator:          alice.String(),
				Symbol:           "NIL-COIN",
				Question:         "Nil money?",
				InitialLiquidity: nil,
				EndBlock:         1000,
			},
			expectErr: true,
		},
		{
			name: "Error - Negative Liquidity",
			msg: types.MsgCreateMarket{
				Creator:          alice.String(),
				Symbol:           "NEG-COIN",
				Question:         "Negative money?",
				InitialLiquidity: testutil.IntPtr(-100),
				EndBlock:         1000,
			},
			expectErr: true,
		},
		{
			name: "Error - Below Min Liquidity",
			msg: types.MsgCreateMarket{
				Creator:  alice.String(),
				Symbol:   "TINY",
				Question: "Too small?",
				// Assuming default params set min liquidity > 1
				InitialLiquidity: testutil.IntPtr(1),
				EndBlock:         1000,
			},
			expectErr: true,
		},
		{
			name: "Error - Invalid Creator Address",
			msg: types.MsgCreateMarket{
				Creator:          "invalid-address",
				Symbol:           "BAD-ADDR",
				Question:         "Who am I?",
				InitialLiquidity: testutil.IntPtr(1000),
				EndBlock:         1000,
			},
			expectErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Init fresh fixture
			f := initFixture(t)
			ms := keeper.NewMsgServerImpl(f.keeper)
			ctx := sdk.UnwrapSDKContext(f.ctx)

			// Set a fixed block height for consistent testing
			ctx = ctx.WithBlockHeight(10)

			// Fund Alice
			f.bankKeeper.SetBalance(alice, sdk.NewCoin("uspark", math.NewInt(100000)))

			res, err := ms.CreateMarket(ctx, &tc.msg)

			if tc.expectErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.NotNil(t, res)

				if tc.checkState != nil {
					tc.checkState(t, f.keeper, f.bankKeeper, ctx, res)
				}
			}
		})
	}
}

// TestMsgCreateMarket_TrustLevelGating covers FUTARCHY-6: once a RepKeeper is
// wired, CreateMarket requires the caller to have ESTABLISHED+ trust level.
// The existing tests above exercise the no-repKeeper (dev) fallback.
func TestMsgCreateMarket_TrustLevelGating(t *testing.T) {
	alice := sdk.AccAddress([]byte("alice_trust_test___"))

	baseMsg := func(symbol string) *types.MsgCreateMarket {
		return &types.MsgCreateMarket{
			Creator:          alice.String(),
			Symbol:           symbol,
			Question:         fmt.Sprintf("%s question?", symbol),
			InitialLiquidity: testutil.IntPtr(100000),
			EndBlock:         200,
		}
	}

	t.Run("rejects PROVISIONAL trust level", func(t *testing.T) {
		f := initFixture(t)
		f.keeper.SetRepKeeper(&stubRepKeeper{trust: reptypes.TrustLevel_TRUST_LEVEL_PROVISIONAL})
		ms := keeper.NewMsgServerImpl(f.keeper)
		ctx := sdk.UnwrapSDKContext(f.ctx).WithBlockHeight(10)
		f.bankKeeper.SetBalance(alice, sdk.NewCoin("uspark", math.NewInt(100000)))

		_, err := ms.CreateMarket(ctx, baseMsg("TRUST-LOW"))
		require.Error(t, err)
		require.Contains(t, err.Error(), "ESTABLISHED+")
	})

	t.Run("rejects when rep lookup fails (not a member)", func(t *testing.T) {
		f := initFixture(t)
		f.keeper.SetRepKeeper(&stubRepKeeper{lookup: fmt.Errorf("member not found")})
		ms := keeper.NewMsgServerImpl(f.keeper)
		ctx := sdk.UnwrapSDKContext(f.ctx).WithBlockHeight(10)
		f.bankKeeper.SetBalance(alice, sdk.NewCoin("uspark", math.NewInt(100000)))

		_, err := ms.CreateMarket(ctx, baseMsg("TRUST-NONE"))
		require.Error(t, err)
		require.Contains(t, err.Error(), "active member")
	})

	t.Run("accepts ESTABLISHED trust level", func(t *testing.T) {
		f := initFixture(t)
		f.keeper.SetRepKeeper(&stubRepKeeper{trust: reptypes.TrustLevel_TRUST_LEVEL_ESTABLISHED})
		ms := keeper.NewMsgServerImpl(f.keeper)
		ctx := sdk.UnwrapSDKContext(f.ctx).WithBlockHeight(10)
		f.bankKeeper.SetBalance(alice, sdk.NewCoin("uspark", math.NewInt(100000)))

		res, err := ms.CreateMarket(ctx, baseMsg("TRUST-OK"))
		require.NoError(t, err)
		require.NotNil(t, res)
	})
}

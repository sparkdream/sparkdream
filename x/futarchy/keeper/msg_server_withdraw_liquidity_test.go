package keeper_test

import (
	"testing"

	"sparkdream/x/futarchy/keeper"
	"sparkdream/x/futarchy/types"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

func TestWithdrawLiquidity(t *testing.T) {
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.keeper)
	ctx := sdk.UnwrapSDKContext(f.ctx)

	creator := sdk.AccAddress("creator_____________")
	nonCreator := sdk.AccAddress("noncreator__________")
	liquidity := sdk.NewCoin("uspark", math.NewInt(100000))

	// Fund creator
	f.bankKeeper.SetBalance(creator, liquidity)

	// Create market
	marketId, err := f.keeper.CreateMarketInternal(ctx, creator, "TEST", "Test Market", 1000, 0, liquidity)
	require.NoError(t, err)

	tests := []struct {
		name      string
		setup     func()
		msg       *types.MsgWithdrawLiquidity
		expectErr bool
		errMsg    string
	}{
		{
			name: "Error - Market not resolved",
			setup: func() {
				// Market is still ACTIVE
			},
			msg: &types.MsgWithdrawLiquidity{
				Creator:  creator.String(),
				MarketId: marketId,
			},
			expectErr: true,
			errMsg:    "must be resolved",
		},
		{
			name: "Error - Non-creator attempting withdrawal",
			setup: func() {
				// Resolve the market first
				market, err := f.keeper.Market.Get(ctx, marketId)
				require.NoError(t, err)
				market.Status = "RESOLVED_YES"
				market.ResolutionHeight = ctx.BlockHeight()
				err = f.keeper.Market.Set(ctx, marketId, market)
				require.NoError(t, err)
			},
			msg: &types.MsgWithdrawLiquidity{
				Creator:  nonCreator.String(),
				MarketId: marketId,
			},
			expectErr: true,
			errMsg:    "only market creator can withdraw",
		},
		{
			name: "Success - Valid withdrawal by creator",
			setup: func() {
				// Market should already be resolved from previous test
			},
			msg: &types.MsgWithdrawLiquidity{
				Creator:  creator.String(),
				MarketId: marketId,
			},
			expectErr: false,
		},
		{
			name: "Error - No liquidity available (already withdrawn)",
			setup: func() {
				// Previous test withdrew all available liquidity
			},
			msg: &types.MsgWithdrawLiquidity{
				Creator:  creator.String(),
				MarketId: marketId,
			},
			expectErr: true,
			errMsg:    "No liquidity available",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setup != nil {
				tt.setup()
			}

			resp, err := msgServer.WithdrawLiquidity(ctx, tt.msg)

			if tt.expectErr {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.errMsg)
				require.Nil(t, resp)
			} else {
				require.NoError(t, err)
				require.NotNil(t, resp)

				// Verify liquidity was withdrawn
				market, err := f.keeper.Market.Get(ctx, tt.msg.MarketId)
				require.NoError(t, err)
				require.True(t, market.LiquidityWithdrawn.GT(math.ZeroInt()))
			}
		})
	}
}

func TestWithdrawLiquidity_WithTrades(t *testing.T) {
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.keeper)
	ctx := sdk.UnwrapSDKContext(f.ctx)

	creator := sdk.AccAddress("creator_____________")
	trader := sdk.AccAddress("trader______________")
	liquidity := sdk.NewCoin("uspark", math.NewInt(200000))
	tradeCoin := sdk.NewCoin("uspark", math.NewInt(10000))

	// Fund accounts
	f.bankKeeper.SetBalance(creator, liquidity)
	f.bankKeeper.SetBalance(trader, tradeCoin)

	// Create market
	marketId, err := f.keeper.CreateMarketInternal(ctx, creator, "TEST", "Test Market", 1000, 0, liquidity)
	require.NoError(t, err)

	// Make a trade
	_, err = msgServer.Trade(ctx, &types.MsgTrade{
		Creator:  trader.String(),
		MarketId: marketId,
		IsYes:    true,
		AmountIn: &tradeCoin.Amount,
	})
	require.NoError(t, err)

	// Resolve market
	market, err := f.keeper.Market.Get(ctx, marketId)
	require.NoError(t, err)
	market.Status = "RESOLVED_YES"
	market.ResolutionHeight = ctx.BlockHeight()
	err = f.keeper.Market.Set(ctx, marketId, market)
	require.NoError(t, err)

	// Withdraw liquidity
	resp, err := msgServer.WithdrawLiquidity(ctx, &types.MsgWithdrawLiquidity{
		Creator:  creator.String(),
		MarketId: marketId,
	})
	require.NoError(t, err)
	require.NotNil(t, resp)

	// Verify withdrawal amount is tracked
	market, err = f.keeper.Market.Get(ctx, marketId)
	require.NoError(t, err)
	require.True(t, market.LiquidityWithdrawn.GT(math.ZeroInt()))

	// Verify withdrawn amount is less than initial liquidity (because shares were minted)
	require.True(t, market.LiquidityWithdrawn.LT(*market.InitialLiquidity))
}

func TestWithdrawLiquidity_NonExistentMarket(t *testing.T) {
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.keeper)
	ctx := sdk.UnwrapSDKContext(f.ctx)

	creator := sdk.AccAddress("creator_____________")

	resp, err := msgServer.WithdrawLiquidity(ctx, &types.MsgWithdrawLiquidity{
		Creator:  creator.String(),
		MarketId: 9999,
	})

	require.Error(t, err)
	require.Contains(t, err.Error(), "not found")
	require.Nil(t, resp)
}

func TestWithdrawLiquidity_DifferentResolutionStatuses(t *testing.T) {
	creator := sdk.AccAddress("creator_____________")
	liquidity := sdk.NewCoin("uspark", math.NewInt(100000))

	statuses := []string{"RESOLVED_YES", "RESOLVED_NO", "RESOLVED_INVALID"}

	for _, status := range statuses {
		t.Run("Withdraw from "+status, func(t *testing.T) {
			// Create fresh fixture for each test
			testFixture := initFixture(t)
			testCtx := sdk.UnwrapSDKContext(testFixture.ctx)
			testFixture.bankKeeper.SetBalance(creator, liquidity)
			testMsgServer := keeper.NewMsgServerImpl(testFixture.keeper)

			// Create market
			marketId, err := testFixture.keeper.CreateMarketInternal(testCtx, creator, "TEST", "Test Market", 1000, 0, liquidity)
			require.NoError(t, err)

			// Resolve market with specific status
			market, err := testFixture.keeper.Market.Get(testCtx, marketId)
			require.NoError(t, err)
			market.Status = status
			market.ResolutionHeight = testCtx.BlockHeight()
			err = testFixture.keeper.Market.Set(testCtx, marketId, market)
			require.NoError(t, err)

			// Withdraw should succeed for all resolved statuses
			resp, err := testMsgServer.WithdrawLiquidity(testCtx, &types.MsgWithdrawLiquidity{
				Creator:  creator.String(),
				MarketId: marketId,
			})

			require.NoError(t, err)
			require.NotNil(t, resp)
		})
	}
}

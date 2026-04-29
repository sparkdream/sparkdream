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

	// FUTARCHY-S2-1: After trades on a YES-resolved market, the creator's
	// claim is the LMSR remainder b * ln(1 + e^((q_no - q_yes)/b)), which is
	// strictly less than InitialLiquidity once any YES trade happens. Prior
	// to the fix the test (incorrectly) asserted full subsidy refund.
	market, err = f.keeper.Market.Get(ctx, marketId)
	require.NoError(t, err)
	require.True(t, market.LiquidityWithdrawn.IsPositive(),
		"creator should receive a positive remainder")
	require.True(t, market.LiquidityWithdrawn.LT(*market.InitialLiquidity),
		"YES-resolved market with YES trades must leave less than InitialLiquidity for the creator (had %s, initial %s)",
		market.LiquidityWithdrawn.String(), market.InitialLiquidity.String())
}

// FUTARCHY-S2-1: prior to the fix, after heavy YES trades + RESOLVED_YES the
// creator could withdraw the full InitialLiquidity, leaving the module short
// of what winning redeemers needed (`q_yes` spark). The corrected residual
// formula bounds the creator's claim to `b * ln(1 + e^((q_no-q_yes)/b))`,
// which together with `q_yes` exactly equals `C(q_yes, q_no)` — the total
// collateral the module holds for this market — so the system is solvent.
func TestWithdrawLiquidity_SolvencyAfterHeavyTrade(t *testing.T) {
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.keeper)
	ctx := sdk.UnwrapSDKContext(f.ctx)

	creator := sdk.AccAddress("creator_____________")
	trader := sdk.AccAddress("trader______________")
	liquidity := sdk.NewCoin("uspark", math.NewInt(200000))
	tradeCoin := sdk.NewCoin("uspark", math.NewInt(150000)) // heavy YES bet

	f.bankKeeper.SetBalance(creator, liquidity)
	f.bankKeeper.SetBalance(trader, tradeCoin)

	marketId, err := f.keeper.CreateMarketInternal(ctx, creator, "TEST", "Heavy YES", 1000, 0, liquidity)
	require.NoError(t, err)

	_, err = msgServer.Trade(ctx, &types.MsgTrade{
		Creator:  trader.String(),
		MarketId: marketId,
		IsYes:    true,
		AmountIn: &tradeCoin.Amount,
	})
	require.NoError(t, err)

	// Resolve YES.
	market, err := f.keeper.Market.Get(ctx, marketId)
	require.NoError(t, err)
	market.Status = "RESOLVED_YES"
	market.ResolutionHeight = ctx.BlockHeight()
	require.NoError(t, f.keeper.Market.Set(ctx, marketId, market))

	resp, err := msgServer.WithdrawLiquidity(ctx, &types.MsgWithdrawLiquidity{
		Creator:  creator.String(),
		MarketId: marketId,
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.NotNil(t, resp.Amount)

	// Solvency invariant: creator residual + winning_pool ≤ initial + trader_in.
	// The mock bank doesn't enforce module-account balance, so we check the
	// math directly: the LMSR identity guarantees equality up to truncation
	// rounding, so residual must be strictly less than InitialLiquidity once
	// any YES trade lands on a YES-resolved market.
	market, err = f.keeper.Market.Get(ctx, marketId)
	require.NoError(t, err)
	withdrawn := *resp.Amount
	require.True(t, withdrawn.IsPositive())
	require.True(t, withdrawn.LT(*market.InitialLiquidity),
		"heavy YES trade + RESOLVED_YES must leave less than InitialLiquidity for the creator (got %s, initial %s)",
		withdrawn.String(), market.InitialLiquidity.String())

	// The leftover (initial - withdrawn) should cover the winning shares the
	// trader received: that's the solvency property.
	leftoverForRedemption := market.InitialLiquidity.Sub(withdrawn).Add(tradeCoin.Amount)
	require.True(t, leftoverForRedemption.GTE(*market.PoolYes),
		"leftover %s must cover winning pool %s", leftoverForRedemption.String(), market.PoolYes.String())
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

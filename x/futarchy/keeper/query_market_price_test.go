package keeper_test

import (
	"testing"

	"sparkdream/testutil"
	"sparkdream/x/futarchy/keeper"
	"sparkdream/x/futarchy/types"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

func TestGetMarketPrice(t *testing.T) {
	f := initFixture(t)
	queryServer := keeper.NewQueryServerImpl(f.keeper)
	ctx := sdk.UnwrapSDKContext(f.ctx)

	creator := sdk.AccAddress("creator_____________")
	liquidity := sdk.NewCoin("uspark", math.NewInt(100000))

	// Fund creator
	f.bankKeeper.SetBalance(creator, liquidity)

	// Create market
	marketId, err := f.keeper.CreateMarketInternal(ctx, creator, "TEST", "Test Market", 1000, 0, liquidity)
	require.NoError(t, err)

	tests := []struct {
		name      string
		req       *types.QueryGetMarketPriceRequest
		expectErr bool
		errMsg    string
		validate  func(*types.QueryGetMarketPriceResponse)
	}{
		{
			name: "Success - YES outcome with default amount",
			req: &types.QueryGetMarketPriceRequest{
				MarketId: marketId,
				IsYes:    true,
				Amount:   nil, // Defaults to 1000 in logic
			},
			expectErr: false,
			validate: func(resp *types.QueryGetMarketPriceResponse) {
				require.NotNil(t, resp.Price)
				require.NotNil(t, resp.SharesOut)

				// Verify price (already *math.LegacyDec)
				require.True(t, resp.Price.GT(math.LegacyZeroDec()))

				// Verify shares out (already *math.Int)
				require.True(t, resp.SharesOut.IsPositive())
			},
		},
		{
			name: "Success - NO outcome with default amount",
			req: &types.QueryGetMarketPriceRequest{
				MarketId: marketId,
				IsYes:    false,
				Amount:   nil,
			},
			expectErr: false,
			validate: func(resp *types.QueryGetMarketPriceResponse) {
				require.NotNil(t, resp.Price)
				require.NotNil(t, resp.SharesOut)

				require.True(t, resp.Price.GT(math.LegacyZeroDec()))
				require.True(t, resp.SharesOut.IsPositive())
			},
		},
		{
			name: "Success - Custom amount",
			req: &types.QueryGetMarketPriceRequest{
				MarketId: marketId,
				IsYes:    true,
				Amount:   testutil.IntPtr(5000),
			},
			expectErr: false,
			validate: func(resp *types.QueryGetMarketPriceResponse) {
				require.NotNil(t, resp.Price)
				require.NotNil(t, resp.SharesOut)

				require.True(t, resp.Price.GT(math.LegacyZeroDec()))
			},
		},
		{
			name: "Error - Negative amount",
			req: &types.QueryGetMarketPriceRequest{
				MarketId: marketId,
				IsYes:    true,
				Amount:   testutil.IntPtr(-100), // Negative check
			},
			expectErr: true,
			errMsg:    "amount cannot be negative",
		},
		{
			name: "Error - Non-existent market",
			req: &types.QueryGetMarketPriceRequest{
				MarketId: 9999,
				IsYes:    true,
				Amount:   nil,
			},
			expectErr: true,
			errMsg:    "not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := queryServer.GetMarketPrice(ctx, tt.req)

			if tt.expectErr {
				require.Error(t, err)
				if tt.errMsg != "" {
					require.Contains(t, err.Error(), tt.errMsg)
				}
				require.Nil(t, resp)
			} else {
				require.NoError(t, err)
				require.NotNil(t, resp)
				if tt.validate != nil {
					tt.validate(resp)
				}
			}
		})
	}
}

func TestGetMarketPrice_NilRequest(t *testing.T) {
	f := initFixture(t)
	queryServer := keeper.NewQueryServerImpl(f.keeper)
	ctx := sdk.UnwrapSDKContext(f.ctx)

	resp, err := queryServer.GetMarketPrice(ctx, nil)

	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid request")
	require.Nil(t, resp)
}

func TestGetMarketPrice_InactiveMarket(t *testing.T) {
	f := initFixture(t)
	queryServer := keeper.NewQueryServerImpl(f.keeper)
	ctx := sdk.UnwrapSDKContext(f.ctx)

	creator := sdk.AccAddress("creator_____________")
	liquidity := sdk.NewCoin("uspark", math.NewInt(100000))

	// Fund creator
	f.bankKeeper.SetBalance(creator, liquidity)

	// Create market
	marketId, err := f.keeper.CreateMarketInternal(ctx, creator, "TEST", "Test Market", 1000, 0, liquidity)
	require.NoError(t, err)

	// Resolve the market (make it inactive)
	market, err := f.keeper.Market.Get(ctx, marketId)
	require.NoError(t, err)
	market.Status = "RESOLVED_YES"
	market.ResolutionHeight = ctx.BlockHeight()
	err = f.keeper.Market.Set(ctx, marketId, market)
	require.NoError(t, err)

	// Try to get price
	resp, err := queryServer.GetMarketPrice(ctx, &types.QueryGetMarketPriceRequest{
		MarketId: marketId,
		IsYes:    true,
		Amount:   nil,
	})

	require.Error(t, err)
	require.Contains(t, err.Error(), "not active")
	require.Nil(t, resp)
}

func TestGetMarketPrice_LargeAmount(t *testing.T) {
	f := initFixture(t)
	queryServer := keeper.NewQueryServerImpl(f.keeper)
	ctx := sdk.UnwrapSDKContext(f.ctx)

	creator := sdk.AccAddress("creator_____________")
	liquidity := sdk.NewCoin("uspark", math.NewInt(100000))

	// Fund creator
	f.bankKeeper.SetBalance(creator, liquidity)

	// Create market
	marketId, err := f.keeper.CreateMarketInternal(ctx, creator, "TEST", "Test Market", 1000, 0, liquidity)
	require.NoError(t, err)

	// Get price with large amount - thanks to numerical stability (ClampExponent),
	// this should succeed but return a very high price
	resp, err := queryServer.GetMarketPrice(ctx, &types.QueryGetMarketPriceRequest{
		MarketId: marketId,
		IsYes:    true,
		Amount:   testutil.IntPtr(50000), // Large relative to liquidity
	})

	require.NoError(t, err)
	require.NotNil(t, resp)

	// Price should be valid and positive
	require.True(t, resp.Price.GT(math.LegacyZeroDec()))
	// Shares out should be positive
	require.True(t, resp.SharesOut.IsPositive())
}

func TestGetMarketPrice_ComparePricesYesVsNo(t *testing.T) {
	f := initFixture(t)
	queryServer := keeper.NewQueryServerImpl(f.keeper)
	ctx := sdk.UnwrapSDKContext(f.ctx)

	creator := sdk.AccAddress("creator_____________")
	liquidity := sdk.NewCoin("uspark", math.NewInt(100000))

	// Fund creator
	f.bankKeeper.SetBalance(creator, liquidity)

	// Create market
	marketId, err := f.keeper.CreateMarketInternal(ctx, creator, "TEST", "Test Market", 1000, 0, liquidity)
	require.NoError(t, err)

	amount := testutil.IntPtr(2000)

	// Get YES price
	yesResp, err := queryServer.GetMarketPrice(ctx, &types.QueryGetMarketPriceRequest{
		MarketId: marketId,
		IsYes:    true,
		Amount:   amount,
	})
	require.NoError(t, err)

	// Get NO price
	noResp, err := queryServer.GetMarketPrice(ctx, &types.QueryGetMarketPriceRequest{
		MarketId: marketId,
		IsYes:    false,
		Amount:   amount,
	})
	require.NoError(t, err)

	// At market initialization, prices should be equal
	yesPrice := *yesResp.Price
	noPrice := *noResp.Price

	// Prices should be very close
	priceDiff := yesPrice.Sub(noPrice).Abs()
	maxDiff := math.LegacyNewDecWithPrec(1, 2) // 0.01 tolerance
	require.True(t, priceDiff.LTE(maxDiff), "YES and NO prices should be similar at initialization")
}

func TestGetMarketPrice_AfterTrade(t *testing.T) {
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.keeper)
	queryServer := keeper.NewQueryServerImpl(f.keeper)
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

	// Get initial YES price
	initialResp, err := queryServer.GetMarketPrice(ctx, &types.QueryGetMarketPriceRequest{
		MarketId: marketId,
		IsYes:    true,
		Amount:   testutil.IntPtr(1000),
	})
	require.NoError(t, err)
	initialPrice := *initialResp.Price

	// Make a YES trade (should increase YES price)
	_, err = msgServer.Trade(ctx, &types.MsgTrade{
		Creator:  trader.String(),
		MarketId: marketId,
		IsYes:    true,
		AmountIn: &tradeCoin.Amount,
	})
	require.NoError(t, err)

	// Get new YES price
	newResp, err := queryServer.GetMarketPrice(ctx, &types.QueryGetMarketPriceRequest{
		MarketId: marketId,
		IsYes:    true,
		Amount:   testutil.IntPtr(1000),
	})
	require.NoError(t, err)
	newPrice := *newResp.Price

	// YES price should have increased after buying YES shares
	require.True(t, newPrice.GT(initialPrice), "YES price should increase after YES trade")
}

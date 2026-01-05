package keeper_test

import (
	"testing"

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
				Amount:   "",
			},
			expectErr: false,
			validate: func(resp *types.QueryGetMarketPriceResponse) {
				require.NotEmpty(t, resp.Price)
				require.NotEmpty(t, resp.SharesOut)

				// Verify price is a valid decimal
				price, err := math.LegacyNewDecFromStr(resp.Price)
				require.NoError(t, err)
				require.True(t, price.GT(math.LegacyZeroDec()))

				// Verify shares out is a valid decimal
				shares, err := math.LegacyNewDecFromStr(resp.SharesOut)
				require.NoError(t, err)
				require.True(t, shares.GT(math.LegacyZeroDec()))
			},
		},
		{
			name: "Success - NO outcome with default amount",
			req: &types.QueryGetMarketPriceRequest{
				MarketId: marketId,
				IsYes:    false,
				Amount:   "",
			},
			expectErr: false,
			validate: func(resp *types.QueryGetMarketPriceResponse) {
				require.NotEmpty(t, resp.Price)
				require.NotEmpty(t, resp.SharesOut)

				price, err := math.LegacyNewDecFromStr(resp.Price)
				require.NoError(t, err)
				require.True(t, price.GT(math.LegacyZeroDec()))

				shares, err := math.LegacyNewDecFromStr(resp.SharesOut)
				require.NoError(t, err)
				require.True(t, shares.GT(math.LegacyZeroDec()))
			},
		},
		{
			name: "Success - Custom amount",
			req: &types.QueryGetMarketPriceRequest{
				MarketId: marketId,
				IsYes:    true,
				Amount:   "5000",
			},
			expectErr: false,
			validate: func(resp *types.QueryGetMarketPriceResponse) {
				require.NotEmpty(t, resp.Price)
				require.NotEmpty(t, resp.SharesOut)

				price, err := math.LegacyNewDecFromStr(resp.Price)
				require.NoError(t, err)
				require.True(t, price.GT(math.LegacyZeroDec()))
			},
		},
		{
			name: "Error - Invalid amount",
			req: &types.QueryGetMarketPriceRequest{
				MarketId: marketId,
				IsYes:    true,
				Amount:   "invalid",
			},
			expectErr: true,
			errMsg:    "invalid amount",
		},
		{
			name: "Error - Non-existent market",
			req: &types.QueryGetMarketPriceRequest{
				MarketId: 9999,
				IsYes:    true,
				Amount:   "",
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
				require.Contains(t, err.Error(), tt.errMsg)
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
		Amount:   "",
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
		Amount:   "50000", // Large relative to liquidity
	})

	require.NoError(t, err)
	require.NotNil(t, resp)

	// Price should be valid and positive
	price, err := math.LegacyNewDecFromStr(resp.Price)
	require.NoError(t, err)
	require.True(t, price.GT(math.LegacyZeroDec()))

	// Shares out should be positive
	shares, err := math.LegacyNewDecFromStr(resp.SharesOut)
	require.NoError(t, err)
	require.True(t, shares.GT(math.LegacyZeroDec()))
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

	amount := "2000"

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

	// At market initialization (50/50 probability), YES and NO prices should be equal
	yesPrice, err := math.LegacyNewDecFromStr(yesResp.Price)
	require.NoError(t, err)
	noPrice, err := math.LegacyNewDecFromStr(noResp.Price)
	require.NoError(t, err)

	// Prices should be very close (allow small difference due to LMSR math)
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
		Amount:   "1000",
	})
	require.NoError(t, err)
	initialPrice, err := math.LegacyNewDecFromStr(initialResp.Price)
	require.NoError(t, err)

	// Make a YES trade (should increase YES price)
	_, err = msgServer.Trade(ctx, &types.MsgTrade{
		Creator:  trader.String(),
		MarketId: marketId,
		IsYes:    true,
		AmountIn: tradeCoin.String(),
	})
	require.NoError(t, err)

	// Get new YES price
	newResp, err := queryServer.GetMarketPrice(ctx, &types.QueryGetMarketPriceRequest{
		MarketId: marketId,
		IsYes:    true,
		Amount:   "1000",
	})
	require.NoError(t, err)
	newPrice, err := math.LegacyNewDecFromStr(newResp.Price)
	require.NoError(t, err)

	// YES price should have increased after buying YES shares
	require.True(t, newPrice.GT(initialPrice), "YES price should increase after YES trade")
}

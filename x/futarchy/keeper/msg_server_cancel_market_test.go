package keeper_test

import (
	"fmt"
	"testing"

	"sparkdream/x/futarchy/keeper"
	"sparkdream/x/futarchy/types"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

func TestCancelMarket(t *testing.T) {
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.keeper)
	ctx := sdk.UnwrapSDKContext(f.ctx)

	creator := sdk.AccAddress("creator_____________")
	liquidity := sdk.NewCoin("uspark", math.NewInt(100000))

	// Fund creator
	f.bankKeeper.SetBalance(creator, liquidity)

	// Create market
	marketId, err := f.keeper.CreateMarketInternal(ctx, creator, "TEST", "Test Market", 1000, 0, liquidity)
	require.NoError(t, err)

	// Get authority address
	authorityAddr, err := f.addressCodec.BytesToString(f.keeper.GetAuthority())
	require.NoError(t, err)

	tests := []struct {
		name      string
		msg       *types.MsgCancelMarket
		expectErr bool
		errMsg    string
	}{
		{
			name: "Success - Valid cancellation by authority",
			msg: &types.MsgCancelMarket{
				Authority: authorityAddr,
				MarketId:  marketId,
				Reason:    "Emergency cancellation for testing",
			},
			expectErr: false,
		},
		{
			name: "Error - Invalid authority",
			msg: &types.MsgCancelMarket{
				Authority: sdk.AccAddress("unauthorized________").String(),
				MarketId:  marketId,
				Reason:    "Unauthorized attempt",
			},
			expectErr: true,
			errMsg:    "invalid authority",
		},
		{
			name: "Error - Non-existent market",
			msg: &types.MsgCancelMarket{
				Authority: authorityAddr,
				MarketId:  9999,
				Reason:    "Test",
			},
			expectErr: true,
			errMsg:    "not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create fresh fixture for each test except the first
			testFixture := f
			testCtx := ctx
			if tt.name != "Success - Valid cancellation by authority" {
				testFixture = initFixture(t)
				testCtx = sdk.UnwrapSDKContext(testFixture.ctx)
				testFixture.bankKeeper.SetBalance(creator, liquidity)
				marketId, err = testFixture.keeper.CreateMarketInternal(testCtx, creator, "TEST", "Test Market", 1000, 0, liquidity)
				require.NoError(t, err)
				msgServer = keeper.NewMsgServerImpl(testFixture.keeper)
			}

			resp, err := msgServer.CancelMarket(testCtx, tt.msg)

			if tt.expectErr {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.errMsg)
				require.Nil(t, resp)
			} else {
				require.NoError(t, err)
				require.NotNil(t, resp)

				// Verify market status is CANCELLED
				market, err := testFixture.keeper.Market.Get(testCtx, tt.msg.MarketId)
				require.NoError(t, err)
				require.Equal(t, "CANCELLED", market.Status)
				require.Equal(t, testCtx.BlockHeight(), market.ResolutionHeight)
			}
		})
	}
}

func TestCancelMarket_WithTrades(t *testing.T) {
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

	// Get authority
	authorityAddr, err := f.addressCodec.BytesToString(f.keeper.GetAuthority())
	require.NoError(t, err)

	// Cancel market
	resp, err := msgServer.CancelMarket(ctx, &types.MsgCancelMarket{
		Authority: authorityAddr,
		MarketId:  marketId,
		Reason:    "Test with trades",
	})
	require.NoError(t, err)
	require.NotNil(t, resp)

	// Verify market is cancelled
	market, err := f.keeper.Market.Get(ctx, marketId)
	require.NoError(t, err)
	require.Equal(t, "CANCELLED", market.Status)

	// FUTARCHY-S2-1 trapped-funds fix: cancellation with outstanding shares
	// must snapshot the LMSR settlement price so holders can redeem.
	require.NotNil(t, market.SettlementPriceYes,
		"CANCELLED market with non-zero pools must have a settlement price snapshot")
	require.True(t, market.SettlementPriceYes.IsPositive())
	require.True(t, market.SettlementPriceYes.LT(math.LegacyOneDec()))

	// Creator's refund must be less than InitialLiquidity (some subsidy was
	// consumed by the trade) but still positive.
	withdrawn := *market.LiquidityWithdrawn
	require.True(t, withdrawn.IsPositive())
	require.True(t, withdrawn.LT(*market.InitialLiquidity),
		"cancellation with trades must not refund full subsidy")

	// The trader's YES shares must redeem at p_yes via Redeem.
	yesDenom := "f/" + uint64ToString(marketId) + "/yes"
	yesBal := f.bankKeeper.GetBalance(ctx, trader, yesDenom)
	require.True(t, yesBal.Amount.IsPositive(), "trader should hold YES shares")

	_, err = msgServer.Redeem(ctx, &types.MsgRedeem{
		Creator:  trader.String(),
		MarketId: marketId,
	})
	require.NoError(t, err, "trader must be able to redeem on CANCELLED market")
}

func uint64ToString(n uint64) string {
	// avoid pulling strconv into the test if not already; existing fmt works too.
	return fmt.Sprintf("%d", n)
}

func TestCancelMarket_AlreadyResolved(t *testing.T) {
	f := initFixture(t)
	msgServer := keeper.NewMsgServerImpl(f.keeper)
	ctx := sdk.UnwrapSDKContext(f.ctx)

	creator := sdk.AccAddress("creator_____________")
	liquidity := sdk.NewCoin("uspark", math.NewInt(100000))

	// Fund creator
	f.bankKeeper.SetBalance(creator, liquidity)

	// Create market
	marketId, err := f.keeper.CreateMarketInternal(ctx, creator, "TEST", "Test Market", 1000, 0, liquidity)
	require.NoError(t, err)

	// Manually resolve the market
	market, err := f.keeper.Market.Get(ctx, marketId)
	require.NoError(t, err)
	market.Status = "RESOLVED_YES"
	market.ResolutionHeight = ctx.BlockHeight()
	err = f.keeper.Market.Set(ctx, marketId, market)
	require.NoError(t, err)

	// Try to cancel
	authorityAddr, err := f.addressCodec.BytesToString(f.keeper.GetAuthority())
	require.NoError(t, err)

	resp, err := msgServer.CancelMarket(ctx, &types.MsgCancelMarket{
		Authority: authorityAddr,
		MarketId:  marketId,
		Reason:    "Try to cancel resolved market",
	})

	// Should fail because market is not ACTIVE
	require.Error(t, err)
	require.Contains(t, err.Error(), "not active")
	require.Nil(t, resp)
}

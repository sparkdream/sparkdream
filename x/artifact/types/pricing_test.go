package types_test

import (
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/artifact/types"
)

func coin(n int64) sdk.Coin { return sdk.NewInt64Coin("uspark", n) }

func TestValidatePricing(t *testing.T) {
	ok := []types.MintPolicy{
		{Price: coin(5)},
		{Pricing: types.MintPricing_MINT_PRICING_LINEAR_SUPPLY, Price: coin(5), PriceStep: math.NewInt(1)},
		{Pricing: types.MintPricing_MINT_PRICING_DUTCH, Price: coin(100), FloorPrice: math.NewInt(10), StartTime: 10, EndTime: 20},
		{Pricing: types.MintPricing_MINT_PRICING_DUTCH, Price: coin(100), FloorPrice: math.ZeroInt(), StartTime: 10, EndTime: 20},
	}
	for i, mp := range ok {
		require.NoError(t, types.ValidatePricing(mp), i)
	}
	bad := []types.MintPolicy{
		{Pricing: 9, Price: coin(1)},
		{Price: coin(1), PriceStep: math.NewInt(1)},
		{Price: coin(1), FloorPrice: math.NewInt(1)},
		{Pricing: types.MintPricing_MINT_PRICING_LINEAR_SUPPLY, Price: coin(1)},
		{Pricing: types.MintPricing_MINT_PRICING_LINEAR_SUPPLY, Price: coin(1), PriceStep: math.NewInt(1), FloorPrice: math.NewInt(1)},
		{Pricing: types.MintPricing_MINT_PRICING_DUTCH, Price: coin(100), FloorPrice: math.NewInt(10)},
		{Pricing: types.MintPricing_MINT_PRICING_DUTCH, Price: coin(100), FloorPrice: math.NewInt(200), StartTime: 1, EndTime: 2},
		{Pricing: types.MintPricing_MINT_PRICING_DUTCH, Price: coin(100), FloorPrice: math.NewInt(1), StartTime: 1, EndTime: 2, PriceStep: math.NewInt(1)},
	}
	for i, mp := range bad {
		require.ErrorIs(t, types.ValidatePricing(mp), types.ErrInvalidMintPricing, i)
	}
}

func TestUnitPriceCurves(t *testing.T) {
	linear := types.MintPolicy{Pricing: types.MintPricing_MINT_PRICING_LINEAR_SUPPLY, Price: coin(100), PriceStep: math.NewInt(10)}
	require.Equal(t, int64(100), types.UnitPrice(linear, 0, 0).Int64())
	require.Equal(t, int64(130), types.UnitPrice(linear, 3, 0).Int64())
	total, maxUnit := types.BatchPrice(linear, 2, 3, 0)
	require.Equal(t, int64(120+130+140), total.Int64())
	require.Equal(t, int64(140), maxUnit.Int64())

	dutch := types.MintPolicy{Pricing: types.MintPricing_MINT_PRICING_DUTCH, Price: coin(1000), FloorPrice: math.NewInt(200), StartTime: 1000, EndTime: 2000}
	require.Equal(t, int64(1000), types.UnitPrice(dutch, 0, 500).Int64(), "before start")
	require.Equal(t, int64(1000), types.UnitPrice(dutch, 0, 1000).Int64(), "at start")
	require.Equal(t, int64(600), types.UnitPrice(dutch, 0, 1500).Int64(), "halfway")
	require.Equal(t, int64(200), types.UnitPrice(dutch, 0, 2000).Int64(), "at end")
	require.Equal(t, int64(200), types.UnitPrice(dutch, 0, 9000).Int64(), "after end")
	total, maxUnit = types.BatchPrice(dutch, 0, 2, 1500)
	require.Equal(t, int64(1200), total.Int64())
	require.Equal(t, int64(600), maxUnit.Int64())

	fixed := types.MintPolicy{Price: coin(7)}
	total, maxUnit = types.BatchPrice(fixed, 99, 3, 0)
	require.Equal(t, int64(21), total.Int64())
	require.Equal(t, int64(7), maxUnit.Int64())
	total, _ = types.BatchPrice(fixed, 0, 0, 0)
	require.True(t, total.IsZero())
}

func TestCurveOpenedAndSameCurve(t *testing.T) {
	c := types.Class{MintPolicy: types.MintPolicy{PublicMintEnabled: true, StartTime: 100, Price: coin(1)}}
	require.False(t, types.CurveOpened(c, 50))
	require.True(t, types.CurveOpened(c, 100))
	c.MintPolicy.PublicMintEnabled = false
	require.False(t, types.CurveOpened(c, 500))
	c.PublicMinted = 1
	require.True(t, types.CurveOpened(c, 0))

	a := types.MintPolicy{Pricing: types.MintPricing_MINT_PRICING_LINEAR_SUPPLY, Price: coin(5), PriceStep: math.NewInt(1), PerAddressLimit: 2}
	b := a
	b.PerAddressLimit, b.PublicMintEnabled = 9, true
	require.True(t, types.SameCurve(a, b), "non-curve fields may change")
	b.PriceStep = math.NewInt(2)
	require.False(t, types.SameCurve(a, b))
}

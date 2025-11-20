package keeper

import (
	"context"

	"sparkdream/x/ecosystem/types"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
)

// SplitFunds performs the 50/30/20 split of the community tax.
func (k Keeper) SplitFunds(ctx context.Context) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	logger := sdkCtx.Logger().With("module", "x/split")

	// 1. Get Parameters
	params, err := k.GetParams(ctx)
	if err != nil || params.CommonsCouncilAddress == "" {
		// If no address is set, we return nil so funds stay in Community Pool safely.
		return nil
	}
	commonsAddr, err := sdk.AccAddressFromBech32(params.CommonsCouncilAddress)
	if err != nil {
		logger.Error("Invalid commons council address", "err", err)
		return nil
	}

	// 2. Define Source & Destinations
	// Source: Distribution Module (Community Pool)
	sourceAddr := k.authKeeper.GetModuleAddress(distrtypes.ModuleName)

	// Destinations
	techAddr := k.authKeeper.GetModuleAddress(govtypes.ModuleName)
	ecoAddr := k.authKeeper.GetModuleAddress(types.ModuleName)

	// 3. Check Balance (The "Pot")
	balance := k.bankKeeper.GetAllBalances(sdkCtx, sourceAddr)
	if balance.IsZero() {
		return nil
	}

	// 4. Calculate and Send
	for _, coin := range balance {
		total := coin.Amount

		// Config: Commons (50%), Tech (30%), Eco (20%)
		commonsAmt := total.MulRaw(50).QuoRaw(100)
		techAmt := total.MulRaw(30).QuoRaw(100)
		ecoAmt := total.Sub(commonsAmt).Sub(techAmt) // Remainder

		// Execute Transfers
		k.safeSend(sdkCtx, sourceAddr, commonsAddr, coin.Denom, commonsAmt)
		k.safeSend(sdkCtx, sourceAddr, techAddr, coin.Denom, techAmt)
		k.safeSend(sdkCtx, sourceAddr, ecoAddr, coin.Denom, ecoAmt)
	}

	return nil
}

// safeSend is a helper to reduce error handling boilerplate
func (k Keeper) safeSend(ctx sdk.Context, from, to sdk.AccAddress, denom string, amount math.Int) {
	if !amount.IsPositive() {
		return
	}
	coins := sdk.NewCoins(sdk.NewCoin(denom, amount))
	if err := k.bankKeeper.SendCoins(ctx, from, to, coins); err != nil {
		logger := ctx.Logger().With("module", "x/split")
		logger.Error("Split transfer failed", "to", to.String(), "amount", amount.String(), "err", err)
	}
}

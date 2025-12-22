package keeper

import (
	"context"

	"sparkdream/x/split/types"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
)

// SplitFunds performs the dynamic split based on registered shares using Collections.
func (k Keeper) SplitFunds(ctx context.Context) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	logger := sdkCtx.Logger().With("module", "x/split")

	// 1. Get Source (Community Pool)
	sourceAddr := k.authKeeper.GetModuleAddress(distrtypes.ModuleName)
	balance := k.bankKeeper.GetAllBalances(sdkCtx, sourceAddr)
	if balance.IsZero() {
		return nil
	}

	// 2. Fetch All Shares using Collections Walk
	var allShares []types.Share

	// Walk iterates over the map.
	err := k.Share.Walk(ctx, nil, func(address string, share types.Share) (bool, error) {
		allShares = append(allShares, share)
		return false, nil
	})
	if err != nil {
		return err
	}

	if len(allShares) == 0 {
		return nil
	}

	// 3. Calculate Total Weight
	var totalWeight uint64
	for _, share := range allShares {
		totalWeight += share.Weight
	}

	if totalWeight == 0 {
		return nil
	}

	// 4. Distribute Funds
	for _, coin := range balance {
		totalAmount := coin.Amount

		// Optimization: Skip dust to save gas
		if totalAmount.LTE(math.NewInt(int64(len(allShares)))) {
			continue
		}

		for _, share := range allShares {
			receiverAddr, err := sdk.AccAddressFromBech32(share.Address)
			if err != nil {
				logger.Error("Invalid receiver address in split shares", "addr", share.Address, "err", err)
				continue
			}

			// Math: (Balance * ShareWeight) / TotalWeight
			shareAmount := totalAmount.Mul(math.NewIntFromUint64(share.Weight)).Quo(math.NewIntFromUint64(totalWeight))

			if shareAmount.IsPositive() {
				k.safeSend(sdkCtx, sourceAddr, receiverAddr, coin.Denom, shareAmount)
			}
		}
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
		ctx.Logger().With("module", "x/split").Error("Split transfer failed", "to", to.String(), "amount", amount.String(), "err", err)
	}
}

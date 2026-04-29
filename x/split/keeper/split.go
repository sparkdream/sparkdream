package keeper

import (
	"context"

	"sparkdream/x/split/types"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// SplitFunds distributes community pool funds to registered share recipients.
// It uses DistributeFromFeePool to ensure only the community pool portion of the
// distribution module account is spent — outstanding validator/delegator rewards
// are left untouched.
func (k Keeper) SplitFunds(ctx context.Context) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	logger := sdkCtx.Logger().With("module", "x/split")

	if k.late.distrKeeper == nil {
		logger.Error("split: distrKeeper not wired; skipping community-pool distribution")
		return nil
	}

	// 1. Fetch All Shares using Collections Walk
	var allShares []types.Share

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

	// 2. Calculate Total Weight
	var totalWeight uint64
	for _, share := range allShares {
		totalWeight += share.Weight
	}

	if totalWeight == 0 {
		return nil
	}

	// 3. Query the actual community pool balance (not the full distribution
	// module account, which also holds outstanding validator/delegator rewards).
	pool, err := k.late.distrKeeper.GetCommunityPool(ctx)
	if err != nil {
		return err
	}
	if pool.IsZero() {
		return nil
	}

	// Truncate DecCoins to whole Coins for distribution.
	poolCoins, _ := pool.TruncateDecimal()
	if poolCoins.IsZero() {
		return nil
	}

	// Minimum distribution threshold: skip if pool balance is below 1 SPARK (1_000_000 uspark).
	// This prevents the community pool from being drained every block with tiny distributions
	// that waste gas and produce negligible economic effect.
	minThreshold := math.NewInt(1_000_000)
	belowThreshold := true
	for _, coin := range poolCoins {
		if coin.Amount.GTE(minThreshold) {
			belowThreshold = false
			break
		}
	}
	if belowThreshold {
		return nil
	}

	for _, coin := range poolCoins {
		totalAmount := coin.Amount

		// Skip dust
		// NOTE: Integer division in share calculation (amount * weight / totalWeight) may leave
		// small amounts undistributed due to rounding. This is expected behavior — the remainder
		// stays in the community pool and will be distributed in subsequent blocks once the pool
		// accumulates above the minimum threshold.
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

			if !shareAmount.IsPositive() {
				continue
			}

			coins := sdk.NewCoins(sdk.NewCoin(coin.Denom, shareAmount))
			if err := k.late.distrKeeper.DistributeFromFeePool(ctx, coins, receiverAddr); err != nil {
				// Community pool exhausted — stop distributing this denom
				logger.Debug("Split distribution stopped: community pool exhausted",
					"share", share.Address, "requested", shareAmount.String(), "err", err)
				break
			}
		}
	}

	return nil
}

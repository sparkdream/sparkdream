package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/split/types"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// MinDistributionAmount is the threshold below which the bond denom is not
// distributed this block (1 SPARK = 1_000_000 base units).
var MinDistributionAmount = math.NewInt(1_000_000)

// SplitFunds distributes the community pool's bond denom to registered share
// recipients. It uses DistributeFromFeePool to ensure only the community pool
// portion of the distribution module account is spent — outstanding
// validator/delegator rewards are left untouched.
//
// Only the bond denom is distributed. Anyone can donate a foreign denom to the
// community pool, so walking every pool denom would let the pool's denom count
// set this BeginBlocker's work. Other denoms stay in the pool for x/gov.
//
// It never halts the chain: a store or pool error is logged and the block
// moves on (the funds stay in the community pool for the next block).
func (k Keeper) SplitFunds(ctx context.Context) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	logger := sdkCtx.Logger().With("module", "x/split")

	if k.late.distrKeeper == nil || k.late.identityKeeper == nil {
		logger.Error("split: distrKeeper or identityKeeper not wired; skipping community-pool distribution")
		return nil
	}

	// 1. Fetch All Shares using Collections Walk
	var allShares []types.Share

	err := k.Share.Walk(ctx, nil, func(address string, share types.Share) (bool, error) {
		allShares = append(allShares, share)
		return false, nil
	})
	if err != nil {
		logger.Error("split: failed to read shares; skipping this block", "err", err)
		return nil
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
	// module account, which also holds outstanding validator/delegator rewards)
	// and truncate the bond denom's DecCoin to whole units for distribution.
	pool, err := k.late.distrKeeper.GetCommunityPool(ctx)
	if err != nil {
		logger.Error("split: failed to read the community pool; skipping this block", "err", err)
		return nil
	}
	denom := k.late.identityKeeper.BondDenom(ctx)
	totalAmount := pool.AmountOf(denom).TruncateInt()

	// Threshold: this prevents the community pool from being drained every
	// block with tiny distributions that waste gas.
	if totalAmount.LT(MinDistributionAmount) {
		return nil
	}

	// Skip dust
	// NOTE: Integer division in share calculation (amount * weight / totalWeight) may leave
	// small amounts undistributed due to rounding. This is expected behavior — the remainder
	// stays in the community pool and will be distributed in subsequent blocks once the pool
	// accumulates above the minimum threshold.
	if totalAmount.LTE(math.NewInt(int64(len(allShares)))) {
		return nil
	}

	// Rotate the starting recipient by block height: when the pool runs
	// short mid-loop, the unpaid recipients are not always the same ones
	// (address order would otherwise bias every shortfall).
	offset := 0
	if h := sdkCtx.BlockHeight(); h > 0 {
		offset = int(h % int64(len(allShares)))
	}

	for i := range allShares {
		share := allShares[(i+offset)%len(allShares)]
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

		coins := sdk.NewCoins(sdk.NewCoin(denom, shareAmount))
		if err := k.late.distrKeeper.DistributeFromFeePool(ctx, coins, receiverAddr); err != nil {
			// Community pool short for this recipient; try the others.
			logger.Debug("Split distribution skipped a recipient: community pool short",
				"share", share.Address, "requested", shareAmount.String(), "err", err)
			continue
		}
		sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
			"split_distribution",
			sdk.NewAttribute("recipient", share.Address),
			sdk.NewAttribute("amount", coins.String()),
			sdk.NewAttribute("weight", fmt.Sprintf("%d", share.Weight)),
		))
	}

	return nil
}

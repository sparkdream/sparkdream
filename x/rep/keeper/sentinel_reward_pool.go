package keeper

import (
	"context"
	"fmt"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/rep/types"
)

// Sentinel SPARK reward pool. Held at SentinelRewardPoolAddress() (a derived
// rep sub-address) to keep it partitioned from tag-budget escrows and gov
// appeal bonds, which are also denominated in uspark and live under x/rep.

// GetSentinelRewardPool returns the current sentinel reward pool size — the
// uspark balance held at SentinelRewardPoolAddress.
func (k Keeper) GetSentinelRewardPool(ctx context.Context) math.Int {
	return k.bankKeeper.GetBalance(ctx, SentinelRewardPoolAddress(), types.RewardDenom).Amount
}

// AddToSentinelRewardPool transfers `amount` of SPARK (uspark) from `sender`
// to the sentinel reward pool address. Intended for spam-tax collectors.
// Zero or negative amounts are rejected.
func (k Keeper) AddToSentinelRewardPool(
	ctx context.Context,
	sender sdk.AccAddress,
	amount math.Int,
) error {
	if !amount.IsPositive() {
		return fmt.Errorf("sentinel reward pool contribution must be positive: %s", amount)
	}
	coins := sdk.NewCoins(sdk.NewCoin(types.RewardDenom, amount))
	return k.bankKeeper.SendCoins(ctx, sender, SentinelRewardPoolAddress(), coins)
}

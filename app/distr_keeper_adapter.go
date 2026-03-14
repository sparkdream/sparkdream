package app

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"
	distrkeeper "github.com/cosmos/cosmos-sdk/x/distribution/keeper"
)

// DistrKeeperAdapter wraps the concrete distrkeeper.Keeper to satisfy the
// split types.DistrKeeper interface, adding GetCommunityPool which the SDK
// keeper only exposes via its FeePool collections item.
type DistrKeeperAdapter struct {
	keeper distrkeeper.Keeper
}

func NewDistrKeeperAdapter(k distrkeeper.Keeper) *DistrKeeperAdapter {
	return &DistrKeeperAdapter{keeper: k}
}

func (a *DistrKeeperAdapter) DistributeFromFeePool(ctx context.Context, amount sdk.Coins, receiveAddr sdk.AccAddress) error {
	return a.keeper.DistributeFromFeePool(ctx, amount, receiveAddr)
}

func (a *DistrKeeperAdapter) GetCommunityPool(ctx context.Context) (sdk.DecCoins, error) {
	feePool, err := a.keeper.FeePool.Get(ctx)
	if err != nil {
		return nil, err
	}
	return feePool.CommunityPool, nil
}

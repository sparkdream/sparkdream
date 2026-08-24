package app

import (
	"context"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	distrkeeper "github.com/cosmos/cosmos-sdk/x/distribution/keeper"
	mintkeeper "github.com/cosmos/cosmos-sdk/x/mint/keeper"
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

// GetCommunityTax exposes the distribution community tax. x/rep multiplies it
// by annual provisions to get the community pool's income rate, which is what
// role_reward_inflation_share is a share of.
func (a *DistrKeeperAdapter) GetCommunityTax(ctx context.Context) (math.LegacyDec, error) {
	return a.keeper.GetCommunityTax(ctx)
}

// MintProvisionsAdapter wraps the concrete mintkeeper.Keeper to satisfy x/rep's
// types.MintKeeper, which needs annual provisions and nothing else. Named apart
// from MintKeeperAdapter in bank_guard.go, which serves x/identity's params
// invariant — same upstream keeper, unrelated slices of it.
type MintProvisionsAdapter struct {
	keeper mintkeeper.Keeper
}

func NewMintProvisionsAdapter(k mintkeeper.Keeper) *MintProvisionsAdapter {
	return &MintProvisionsAdapter{keeper: k}
}

// AnnualProvisions returns supply x current inflation, as recomputed by mint's
// BeginBlocker each block.
func (a *MintProvisionsAdapter) AnnualProvisions(ctx context.Context) (math.LegacyDec, error) {
	minter, err := a.keeper.Minter.Get(ctx)
	if err != nil {
		return math.LegacyZeroDec(), err
	}
	return minter.AnnualProvisions, nil
}

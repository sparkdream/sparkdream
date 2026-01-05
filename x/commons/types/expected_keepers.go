package types

import (
	"context"

	"cosmossdk.io/core/address"
	upgradetypes "cosmossdk.io/x/upgrade/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// GroupKeeper defines the expected interface for the Group module.
type GroupKeeper interface {
	GetGroupSequence(context.Context) uint64
	// Methods imported from account should be defined here
}

// AuthKeeper defines the expected interface for the Auth module.
type AuthKeeper interface {
	AddressCodec() address.Codec
	GetAccount(context.Context, sdk.AccAddress) sdk.AccountI // only used for simulation
	// Methods imported from account should be defined here
	GetModuleAddress(string) sdk.AccAddress
	IterateAccounts(context.Context, func(sdk.AccountI) bool)
}

// BankKeeper defines the expected interface for the Bank module.
type BankKeeper interface {
	SpendableCoins(context.Context, sdk.AccAddress) sdk.Coins
	// Methods imported from bank should be defined here
	GetAllBalances(context.Context, sdk.AccAddress) sdk.Coins
	MintCoins(context.Context, string, sdk.Coins) error
	SendCoins(context.Context, sdk.AccAddress, sdk.AccAddress, sdk.Coins) error
	SendCoinsFromAccountToModule(context.Context, sdk.AccAddress, string, sdk.Coins) error
	SendCoinsFromModuleToAccount(context.Context, string, sdk.AccAddress, sdk.Coins) error
}

// FutarchyKeeper defines the expected interface for the FutarchyKeeper module.
type FutarchyKeeper interface {
	CreateMarketInternal(sdk.Context, sdk.AccAddress, string, string, int64, int64, sdk.Coin) (uint64, error)
}

// SplitKeeper defines the expected interface for the Split module.
type SplitKeeper interface {
	SetShareByAddress(context.Context, string, uint64)
}

// UpgradeKeeper defines the expected interface for the Upgrade module.
type UpgradeKeeper interface {
	ScheduleUpgrade(context.Context, upgradetypes.Plan) error
}

// ParamSubspace defines the expected Subspace interface for parameters.
type ParamSubspace interface {
	Get(context.Context, []byte, interface{})
	Set(context.Context, []byte, interface{})
}

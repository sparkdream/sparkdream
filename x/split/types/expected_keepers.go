package types

import (
	"context"

	commonstypes "sparkdream/x/commons/types"

	"cosmossdk.io/core/address"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

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
	SendCoins(context.Context, sdk.AccAddress, sdk.AccAddress, sdk.Coins) error
	SendCoinsFromModuleToAccount(context.Context, string, sdk.AccAddress, sdk.Coins) error
}

// CommonsKeeper defines the expected interface for the Commons module.
type CommonsKeeper interface {
	GetParams(context.Context) (commonstypes.Params, error)
}

// ParamSubspace defines the expected Subspace interface for parameters.
type ParamSubspace interface {
	Get(context.Context, []byte, interface{})
	Set(context.Context, []byte, interface{})
}

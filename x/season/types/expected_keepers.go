package types

import (
	"context"

	"cosmossdk.io/core/address"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// AuthKeeper defines the expected interface for the Auth module.
type AuthKeeper interface {
	AddressCodec() address.Codec
	GetAccount(context.Context, sdk.AccAddress) sdk.AccountI // only used for simulation
	// Methods imported from account should be defined here
}

// BankKeeper defines the expected interface for the Bank module.
type BankKeeper interface {
	SpendableCoins(context.Context, sdk.AccAddress) sdk.Coins
	// Methods imported from bank should be defined here
}

// ParamSubspace defines the expected Subspace interface for parameters.
type ParamSubspace interface {
	Get(context.Context, []byte, interface{})
	Set(context.Context, []byte, interface{})
}

// RepKeeper defines the expected interface for the x/rep module.
// This enables cross-module integration for DREAM token operations and jury reviews.
type RepKeeper interface {
	// DREAM token operations
	GetBalance(ctx context.Context, addr sdk.AccAddress) (math.Int, error)
	BurnDREAM(ctx context.Context, addr sdk.AccAddress, amount math.Int) error
	LockDREAM(ctx context.Context, addr sdk.AccAddress, amount math.Int) error
	UnlockDREAM(ctx context.Context, addr sdk.AccAddress, amount math.Int) error

	// Membership verification
	IsMember(ctx context.Context, addr string) bool
	GetMember(ctx context.Context, addr string) (interface{}, error)
}

// NameKeeper defines the expected interface for the x/name module.
// This enables cross-module integration for name reservation and release.
type NameKeeper interface {
	// Name operations
	GetName(ctx context.Context, name string) (interface{}, bool)
	SetName(ctx context.Context, record interface{}) error
	RemoveNameFromOwner(ctx context.Context, owner sdk.AccAddress, name string) error

	// Check if a name is available
	IsNameAvailable(ctx context.Context, name string) bool
}

// CommonsKeeper defines the expected interface for the x/commons module.
// This enables cross-module integration for committee membership checks.
type CommonsKeeper interface {
	// Committee membership checks
	IsCommitteeMember(ctx context.Context, address sdk.AccAddress, council string, committee string) (bool, error)
}

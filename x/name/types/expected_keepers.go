package types

import (
	"context"

	commonstypes "sparkdream/x/commons/types"

	"cosmossdk.io/core/address"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// AuthKeeper defines the expected interface for the Auth module.
type AuthKeeper interface {
	AddressCodec() address.Codec
	GetAccount(context.Context, sdk.AccAddress) sdk.AccountI // only used for simulation
}

// BankKeeper defines the expected interface for the Bank module.
type BankKeeper interface {
	SpendableCoins(context.Context, sdk.AccAddress) sdk.Coins
	SendCoins(context.Context, sdk.AccAddress, sdk.AccAddress, sdk.Coins) error
	SendCoinsFromAccountToModule(context.Context, sdk.AccAddress, string, sdk.Coins) error
}

// CommonsKeeper defines the expected interface for the x/commons module.
type CommonsKeeper interface {
	GetGroup(context.Context, string) (commonstypes.Group, error)
	SetGroup(context.Context, string, commonstypes.Group) error
	GetPolicyPermissions(context.Context, string) (commonstypes.PolicyPermissions, error)
	SetPolicyPermissions(context.Context, string, commonstypes.PolicyPermissions) error
	// IsCouncilAuthorized checks if addr is authorized via governance, council policy,
	// or committee membership.
	IsCouncilAuthorized(ctx context.Context, addr string, council string, committee string) bool
	// HasMember checks if an address is a member of a council.
	HasMember(ctx context.Context, councilName string, address string) (bool, error)
	// AddMember adds a member to a council.
	AddMember(ctx context.Context, councilName string, member commonstypes.Member) error
}

// RepKeeper defines the expected interface for the x/rep module (DREAM token operations).
type RepKeeper interface {
	LockDREAM(ctx context.Context, addr sdk.AccAddress, amount math.Int) error
	UnlockDREAM(ctx context.Context, addr sdk.AccAddress, amount math.Int) error
	BurnDREAM(ctx context.Context, addr sdk.AccAddress, amount math.Int) error
}

// ParamSubspace defines the expected Subspace interface for parameters.
type ParamSubspace interface {
	Get(context.Context, []byte, interface{})
	Set(context.Context, []byte, interface{})
}

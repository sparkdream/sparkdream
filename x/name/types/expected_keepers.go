package types

import (
	"context"

	commonstypes "sparkdream/x/commons/types"

	"cosmossdk.io/core/address"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/group"
)

// GroupKeeper defines the expected interface for the Group module.
type GroupKeeper interface {
	GetGroupSequence(sdk.Context) uint64
	// Methods imported from account should be defined here
	GroupPoliciesByGroup(context.Context, *group.QueryGroupPoliciesByGroupRequest) (*group.QueryGroupPoliciesByGroupResponse, error)
	GroupsByMember(context.Context, *group.QueryGroupsByMemberRequest) (*group.QueryGroupsByMemberResponse, error)
}

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
	SendCoins(context.Context, sdk.AccAddress, sdk.AccAddress, sdk.Coins) error
	SendCoinsFromAccountToModule(context.Context, sdk.AccAddress, string, sdk.Coins) error
}

// CommonsKeeper defines the expected interface for the x/commons module.
type CommonsKeeper interface {
	GetExtendedGroup(context.Context, string) (commonstypes.ExtendedGroup, error)
	SetExtendedGroup(context.Context, string, commonstypes.ExtendedGroup) error
	GetPolicyPermissions(context.Context, string) (commonstypes.PolicyPermissions, error)
	SetPolicyPermissions(context.Context, string, commonstypes.PolicyPermissions) error
}

// ExtendedGroup is a local proxy struct for the type defined x/commons.
type ExtendedGroup struct {
	GroupId       uint64
	PolicyAddress string
}

// PolicyPermissions is a local proxy struct for the type defined x/commons.
type PolicyPermissions struct {
	PolicyAddress   string
	AllowedMessages []string
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

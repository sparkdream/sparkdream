package types

import (
	"context"

	"cosmossdk.io/core/address"
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

// ParamSubspace defines the expected Subspace interface for parameters.
type ParamSubspace interface {
	Get(context.Context, []byte, interface{})
	Set(context.Context, []byte, interface{})
}

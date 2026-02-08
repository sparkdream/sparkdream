package types

import (
	"context"

	"cosmossdk.io/core/address"
	sdk "github.com/cosmos/cosmos-sdk/types"

	seasontypes "sparkdream/x/season/types"
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

// CommonsKeeper defines the expected interface for the Commons module.
type CommonsKeeper interface {
	// Check if an address is a member of a specific committee in a council
	IsCommitteeMember(ctx context.Context, address sdk.AccAddress, council string, committee string) (bool, error)

	// Get the group info for a committee
	GetCommitteeGroupInfo(ctx context.Context, council string, committee string) (interface{}, error)
}

// SeasonKeeper defines the expected interface for the Season module.
type SeasonKeeper interface {
	// GetCurrentSeason returns the current season state
	GetCurrentSeason(ctx context.Context) (seasontypes.Season, error)
	// ResolveDisplayNameAppealInternal resolves a display name appeal after jury verdict
	ResolveDisplayNameAppealInternal(ctx context.Context, member string, appealSucceeded bool) error
}

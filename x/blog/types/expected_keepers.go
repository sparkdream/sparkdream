package types

import (
	"context"

	"cosmossdk.io/core/address"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	reptypes "sparkdream/x/rep/types"
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
	SendCoinsFromAccountToModule(ctx context.Context, senderAddr sdk.AccAddress, recipientModule string, amt sdk.Coins) error
	BurnCoins(ctx context.Context, moduleName string, amt sdk.Coins) error
}

// CommonsKeeper defines the expected interface for the Commons module.
// Used for council-gated operational parameter updates.
type CommonsKeeper interface {
	// IsCouncilAuthorized checks if addr is authorized via governance, council policy,
	// or committee membership.
	IsCouncilAuthorized(ctx context.Context, addr string, council string, committee string) bool
	// SpendFromTreasury transfers coins from a council treasury to a recipient.
	SpendFromTreasury(ctx context.Context, council string, recipient sdk.AccAddress, amount sdk.Coins) error
}

// RepKeeper defines the expected interface for the Rep module.
// Required dependency — x/blog requires x/rep for membership and trust level checks.
type RepKeeper interface {
	// IsActiveMember returns true if addr is an active member.
	IsActiveMember(ctx context.Context, addr sdk.AccAddress) bool
	// GetTrustLevel returns the trust level (0-4) for the given address.
	// Returns 0 if the address is not a member.
	GetTrustLevel(ctx context.Context, addr sdk.AccAddress) (reptypes.TrustLevel, error)

	// Content conviction staking & author bonds
	GetContentConviction(ctx context.Context, targetType reptypes.StakeTargetType, targetID uint64) (math.LegacyDec, error)
	GetContentStakes(ctx context.Context, targetType reptypes.StakeTargetType, targetID uint64) ([]reptypes.Stake, error)
	CreateAuthorBond(ctx context.Context, author sdk.AccAddress, targetType reptypes.StakeTargetType, targetID uint64, amount math.Int) (uint64, error)
	SlashAuthorBond(ctx context.Context, targetType reptypes.StakeTargetType, targetID uint64) error
	GetAuthorBond(ctx context.Context, targetType reptypes.StakeTargetType, targetID uint64) (reptypes.Stake, error)

	// Initiative reference validation and linking for conviction propagation
	ValidateInitiativeReference(ctx context.Context, initiativeID uint64) error
	RegisterContentInitiativeLink(ctx context.Context, initiativeID uint64, targetType int32, targetID uint64) error
	RemoveContentInitiativeLink(ctx context.Context, initiativeID uint64, targetType int32, targetID uint64) error

	// Tag registry (owned by x/rep). Tags referenced on posts must already exist
	// in the registry and must not be reserved. IncrementTagUsage bumps the
	// usage_count and last_used_at for a tag when a post references it.
	TagExists(ctx context.Context, name string) (bool, error)
	IsReservedTag(ctx context.Context, name string) (bool, error)
	IncrementTagUsage(ctx context.Context, name string, timestamp int64) error
}

// ParamSubspace defines the expected Subspace interface for parameters.
type ParamSubspace interface {
	Get(context.Context, []byte, interface{})
	Set(context.Context, []byte, interface{})
}

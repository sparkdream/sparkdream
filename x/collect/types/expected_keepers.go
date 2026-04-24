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
}

// BankKeeper defines the expected interface for the Bank module.
type BankKeeper interface {
	SpendableCoins(context.Context, sdk.AccAddress) sdk.Coins
	SendCoins(ctx context.Context, fromAddr sdk.AccAddress, toAddr sdk.AccAddress, amt sdk.Coins) error
	SendCoinsFromAccountToModule(ctx context.Context, senderAddr sdk.AccAddress, recipientModule string, amt sdk.Coins) error
	SendCoinsFromModuleToAccount(ctx context.Context, senderModule string, recipientAddr sdk.AccAddress, amt sdk.Coins) error
	BurnCoins(ctx context.Context, moduleName string, amt sdk.Coins) error
}

// RepKeeper defines the expected interface for the x/rep module.
// Used for membership checks, trust level verification, curator bonding, and jury resolution.
type RepKeeper interface {
	// Membership and trust level
	IsMember(ctx context.Context, addr sdk.AccAddress) bool
	GetTrustLevel(ctx context.Context, addr sdk.AccAddress) (reptypes.TrustLevel, error)

	// DREAM token operations (for curator bonds and challenge deposits)
	LockDREAM(ctx context.Context, addr sdk.AccAddress, amount math.Int) error
	UnlockDREAM(ctx context.Context, addr sdk.AccAddress, amount math.Int) error
	BurnDREAM(ctx context.Context, addr sdk.AccAddress, amount math.Int) error

	// Content conviction staking & author bonds
	GetContentConviction(ctx context.Context, targetType reptypes.StakeTargetType, targetID uint64) (math.LegacyDec, error)
	GetContentStakes(ctx context.Context, targetType reptypes.StakeTargetType, targetID uint64) ([]reptypes.Stake, error)
	CreateAuthorBond(ctx context.Context, author sdk.AccAddress, targetType reptypes.StakeTargetType, targetID uint64, amount math.Int) (uint64, error)
	SlashAuthorBond(ctx context.Context, targetType reptypes.StakeTargetType, targetID uint64) error
	GetAuthorBond(ctx context.Context, targetType reptypes.StakeTargetType, targetID uint64) (reptypes.Stake, error)

	// Cross-module conviction propagation
	ValidateInitiativeReference(ctx context.Context, initiativeID uint64) error
	RegisterContentInitiativeLink(ctx context.Context, initiativeID uint64, targetType int32, targetID uint64) error
	RemoveContentInitiativeLink(ctx context.Context, initiativeID uint64, targetType int32, targetID uint64) error

	// Tag registry (owned by x/rep). Tags referenced on collections and
	// reviews must already exist and must not be reserved. IncrementTagUsage
	// bumps usage_count and last_used_at when the module references a tag.
	TagExists(ctx context.Context, name string) (bool, error)
	IsReservedTag(ctx context.Context, name string) (bool, error)
	IncrementTagUsage(ctx context.Context, name string, timestamp int64) error

	// Bonded-role accountability (owned by x/rep). Curator is the collect
	// role keyed as ROLE_TYPE_COLLECT_CURATOR.
	GetBondedRole(ctx context.Context, roleType reptypes.RoleType, addr string) (reptypes.BondedRole, error)
	ReserveBond(ctx context.Context, roleType reptypes.RoleType, addr string, amount math.Int) error
	ReleaseBond(ctx context.Context, roleType reptypes.RoleType, addr string, amount math.Int) error
	SlashBond(ctx context.Context, roleType reptypes.RoleType, addr string, amount math.Int, reason string) error
	RecordActivity(ctx context.Context, roleType reptypes.RoleType, addr string) error
	SetBondStatus(ctx context.Context, roleType reptypes.RoleType, addr string, status reptypes.BondedRoleStatus, cooldownUntil int64) error
	SetBondedRoleConfig(ctx context.Context, cfg reptypes.BondedRoleConfig) error
}

// CommonsKeeper defines the expected interface for the x/commons module.
// Used for council authorization on operational parameter updates.
type CommonsKeeper interface {
	// IsCouncilAuthorized checks if an address is authorized via council/committee
	// Accepts x/gov authority, Commons Council policy address, or Operations Committee member
	IsCouncilAuthorized(ctx context.Context, addr string, council string, committee string) bool
}

// BlogKeeper defines the expected interface for the x/blog module.
// Used for validating OnChainReference entries pointing to blog posts/replies.
// This is optional — if nil, blog references are accepted without validation.
type BlogKeeper interface {
	HasPost(ctx context.Context, id uint64) bool
	HasReply(ctx context.Context, id uint64) bool
}

// ForumKeeper defines the expected interface for the x/forum module.
// Used for sentinel bond operations in content moderation (MsgHideContent)
// and OnChainReference validation for forum posts/replies.
// This is optional — if nil, sentinel operations return ErrNotSentinel.
type ForumKeeper interface {
	// IsSentinelActive checks if the address is an active sentinel (bonded, not demoted, not in cooldown).
	IsSentinelActive(ctx context.Context, sentinel string) (bool, error)

	// GetAvailableBond returns the sentinel's available bond (total bond - committed across all modules).
	GetAvailableBond(ctx context.Context, sentinel string) (math.Int, error)

	// CommitBond commits a portion of the sentinel's bond for a moderation action.
	CommitBond(ctx context.Context, sentinel string, amount math.Int, module string, referenceID uint64) error

	// ReleaseBondCommitment releases a previously committed bond amount back to the sentinel.
	ReleaseBondCommitment(ctx context.Context, sentinel string, amount math.Int, module string, referenceID uint64) error

	// SlashBondCommitment slashes a committed bond amount from the sentinel (burned).
	SlashBondCommitment(ctx context.Context, sentinel string, amount math.Int, module string, referenceID uint64) error

	// Post existence check for OnChainReference validation.
	// Forum replies are posts with ParentId > 0, both stored in Post collection.
	HasPost(ctx context.Context, id uint64) bool
}

// ParamSubspace defines the expected Subspace interface for parameters.
type ParamSubspace interface {
	Get(context.Context, []byte, interface{})
	Set(context.Context, []byte, interface{})
}

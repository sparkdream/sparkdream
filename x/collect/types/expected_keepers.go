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

// IdentityKeeper is the subset of x/identity that collect reads to
// resolve the chain's bond denom at runtime. Late-bound via
// SetIdentityKeeper from app.go. Required: collect panics on first denom
// lookup if identity isn't wired.
type IdentityKeeper interface {
	IsIdentityKeeper() // marker — disambiguates from rep/session.Keeper for depinject
	BondDenom(ctx context.Context) string
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

	// Per-tag reputation deduction, floored at zero. Used on slash paths
	// (endorsement burn, collaborator-stake burn, author bond slash) so the
	// rep score reflects the same accountability event as the DREAM burn.
	DeductReputation(ctx context.Context, memberAddr sdk.AccAddress, tag string, amount math.LegacyDec) error

	// Content conviction staking & author bonds
	GetContentConviction(ctx context.Context, targetType reptypes.StakeTargetType, targetID uint64) (math.LegacyDec, error)
	GetContentStakes(ctx context.Context, targetType reptypes.StakeTargetType, targetID uint64) ([]reptypes.Stake, error)
	CreateAuthorBond(ctx context.Context, author sdk.AccAddress, targetType reptypes.StakeTargetType, targetID uint64, amount math.Int) (uint64, error)
	SlashAuthorBond(ctx context.Context, targetType reptypes.StakeTargetType, targetID uint64) error
	GetAuthorBond(ctx context.Context, targetType reptypes.StakeTargetType, targetID uint64) (reptypes.Stake, error)

	// RestoreAuthorBond is the inverse of SlashAuthorBond, used by the
	// sentinel self-correct path (MsgUnhideContent). Mints the snapshotted
	// amount back to the author and re-locks it as a fresh bond. Idempotent
	// when a bond already exists for the target.
	RestoreAuthorBond(ctx context.Context, author sdk.AccAddress, targetType reptypes.StakeTargetType, targetID uint64, amount math.Int) error

	// AddReputation is the inverse of DeductReputation, used by hide
	// reversal paths (self-correct, jury overturn, appeal timeout) to
	// restore the per-tag rep penalty applied at hide time.
	AddReputation(ctx context.Context, memberAddr sdk.AccAddress, tag string, amount math.LegacyDec) error

	// GetReputationScores returns the member's per-tag reputation map
	// (LegacyDec strings). Used at hide time to snapshot the ACTUAL
	// deducted amount per tag — DeductReputation floors at zero, so
	// restoring the raw penalty param would mint reputation from nothing
	// for authors with less rep than the penalty.
	GetReputationScores(ctx context.Context, addr string) (map[string]string, error)

	// Cross-module conviction propagation
	ValidateInitiativeReference(ctx context.Context, initiativeID uint64) error
	RegisterContentInitiativeLink(ctx context.Context, initiativeID uint64, targetType int32, targetID uint64) error
	RemoveContentInitiativeLink(ctx context.Context, initiativeID uint64, targetType int32, targetID uint64) error

	// Tag registry (owned by x/rep). Tags referenced on collections and
	// reviews must already exist and must not be reserved. IncrementTagUsage
	// bumps usage_count and last_used_at when the module references a tag;
	// DecrementTagUsage decrements usage_count (floor 0) when the reference is
	// released (collection-update drop, etc.).
	TagExists(ctx context.Context, name string) (bool, error)
	IsReservedTag(ctx context.Context, name string) (bool, error)
	IncrementTagUsage(ctx context.Context, name string, timestamp int64) error
	DecrementTagUsage(ctx context.Context, name string) error

	// Bonded-role accountability (owned by x/rep). Curators are keyed as
	// ROLE_TYPE_COLLECT_CURATOR; the moderation sentinel role for hide-content
	// is the shared ROLE_TYPE_CONTENT_SENTINEL.
	GetBondedRole(ctx context.Context, roleType reptypes.RoleType, addr string) (reptypes.BondedRole, error)
	// EligibleForRole is the shared action-time eligibility gate
	// (NORMAL/RECOVERY; UNBONDING while the staying bond covers the role's
	// configured min_bond; DEMOTED never). Hoisted from forum's
	// eligibleSentinel so collect enforces the same gate on hide actions.
	// Typed errors: ErrBondedRoleNotFound, ErrRoleDemoted,
	// ErrRoleUnbondingBelowMin.
	EligibleForRole(ctx context.Context, roleType reptypes.RoleType, addr string) (reptypes.BondedRole, error)

	// Shared RoleActivity surface (owned by x/rep): collect reports hide
	// actions, appeal filings, and jury outcomes, and consumes the shared
	// overturn cooldown. See docs/x-rep-spec.md (RoleActivity).
	RecordRoleAction(ctx context.Context, roleType reptypes.RoleType, addr, kind string) error
	RecordRoleOutcome(ctx context.Context, roleType reptypes.RoleType, addr, kind string, upheld bool) error
	RoleOverturnCooldownUntil(ctx context.Context, roleType reptypes.RoleType, addr string) int64
	GetAvailableBond(ctx context.Context, roleType reptypes.RoleType, addr string) (math.Int, error)
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
// Used solely for OnChainReference validation against forum posts/replies.
// Optional — if nil, forum-typed references are accepted without validation.
//
// Sentinel accountability does NOT flow through this interface: the shared
// CONTENT_SENTINEL bond, eligibility, cooldown, and activity/outcome
// reporting all live on RepKeeper (RoleActivity API).
type ForumKeeper interface {
	// HasPost reports whether a forum post (or reply, since replies are posts
	// with ParentId > 0) exists under the given id.
	HasPost(ctx context.Context, id uint64) bool
	// HasCategory disambiguates this from x/blog's keeper, which also has
	// HasPost. depinject sees both keepers as satisfying ForumKeeper without
	// this discriminator and errors with "Multiple implementations found".
	HasCategory(ctx context.Context, id uint64) bool
}

// ParamSubspace defines the expected Subspace interface for parameters.
type ParamSubspace interface {
	Get(context.Context, []byte, interface{})
	Set(context.Context, []byte, interface{})
}

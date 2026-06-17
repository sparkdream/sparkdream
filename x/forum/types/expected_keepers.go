package types

import (
	"context"

	"cosmossdk.io/core/address"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	commonstypes "sparkdream/x/commons/types"
	reptypes "sparkdream/x/rep/types"
)

// IdentityKeeper is the subset of x/identity that forum reads to resolve
// the chain's bond denom at runtime. Late-bound via SetIdentityKeeper
// from app.go; keeper.BondDenom panics if unwired.
type IdentityKeeper interface {
	IsIdentityKeeper() // marker — disambiguates from rep/session.Keeper for depinject
	BondDenom(ctx context.Context) string
}

// AuthKeeper defines the expected interface for the Auth module.
type AuthKeeper interface {
	AddressCodec() address.Codec
	GetAccount(context.Context, sdk.AccAddress) sdk.AccountI // only used for simulation
	// Methods imported from account should be defined here
}

// CommonsKeeper defines the expected interface for the Commons module.
// Used for group membership and policy verification.
type CommonsKeeper interface {
	// IsGroupPolicyMember checks if an address is a member of a group via its policy address.
	// The policyAddr is the group policy account address.
	IsGroupPolicyMember(ctx context.Context, policyAddr string, memberAddr string) (bool, error)

	// IsGroupPolicyAddress checks if the given address is a valid group policy address.
	IsGroupPolicyAddress(ctx context.Context, addr string) bool

	// IsCouncilAuthorized checks if addr is authorized via governance, council policy,
	// or committee membership. council: "commons"/"technical"/"ecosystem",
	// committee: "operations"/"governance"/"hr".
	IsCouncilAuthorized(ctx context.Context, addr string, council string, committee string) bool

	// GetCategory returns a shared content category by id and whether it exists.
	GetCategory(ctx context.Context, id uint64) (commonstypes.Category, bool)

	// HasCategory reports whether a category exists.
	HasCategory(ctx context.Context, id uint64) bool
}

// BankKeeper defines the expected interface for the Bank module.
type BankKeeper interface {
	SpendableCoins(context.Context, sdk.AccAddress) sdk.Coins
	SendCoins(ctx context.Context, fromAddr sdk.AccAddress, toAddr sdk.AccAddress, amt sdk.Coins) error
	SendCoinsFromAccountToModule(ctx context.Context, senderAddr sdk.AccAddress, recipientModule string, amt sdk.Coins) error
	SendCoinsFromModuleToAccount(ctx context.Context, senderModule string, recipientAddr sdk.AccAddress, amt sdk.Coins) error
	SendCoinsFromModuleToModule(ctx context.Context, senderModule, recipientModule string, amt sdk.Coins) error
	BurnCoins(ctx context.Context, moduleName string, amt sdk.Coins) error
	MintCoins(ctx context.Context, moduleName string, amt sdk.Coins) error
}

// ParamSubspace defines the expected Subspace interface for parameters.
type ParamSubspace interface {
	Get(context.Context, []byte, interface{})
	Set(context.Context, []byte, interface{})
}

// RepKeeper defines the expected interface for the Rep module.
// This interface provides access to DREAM token operations and member management.
type RepKeeper interface {
	// DREAM token operations
	MintDREAM(ctx context.Context, addr sdk.AccAddress, amount math.Int) error
	BurnDREAM(ctx context.Context, addr sdk.AccAddress, amount math.Int) error
	LockDREAM(ctx context.Context, addr sdk.AccAddress, amount math.Int) error
	UnlockDREAM(ctx context.Context, addr sdk.AccAddress, amount math.Int) error
	GetBalance(ctx context.Context, addr sdk.AccAddress) (math.Int, error)
	TransferDREAM(ctx context.Context, sender, recipient sdk.AccAddress, amount math.Int, purpose reptypes.TransferPurpose) error

	// Member queries
	IsMember(ctx context.Context, addr sdk.AccAddress) bool
	IsActiveMember(ctx context.Context, addr sdk.AccAddress) bool
	GetMember(ctx context.Context, addr sdk.AccAddress) (reptypes.Member, error)
	GetTrustLevel(ctx context.Context, addr sdk.AccAddress) (reptypes.TrustLevel, error)
	GetReputationTier(ctx context.Context, addr sdk.AccAddress) (uint64, error)

	// Member moderation
	ZeroMember(ctx context.Context, memberAddr sdk.AccAddress, reason string) error
	DemoteMember(ctx context.Context, memberAddr sdk.AccAddress, reason string) error

	// Appeal initiatives
	// CreateAppealInitiative creates a special initiative for jury-based appeal resolution.
	// initiativeType: type of appeal ("moderation_appeal", "sentinel_appeal", etc.)
	// payload: JSON-encoded appeal data
	// deadline: block height by which the appeal must be resolved
	// Returns the initiative ID or error.
	CreateAppealInitiative(ctx context.Context, initiativeType string, payload []byte, deadline int64) (uint64, error)

	// CreateGovActionAppeal charges the appellant the standard appeal bond,
	// opens a jury initiative, and records a pending GovActionAppeal that
	// resolves through x/rep's ResolveGovActionAppeal. Used by DisputePin to
	// route reply-pin disputes (ActionType REPLY_PIN) through the same audited
	// resolution path as lock/move/hide. Returns (appealID, initiativeID).
	CreateGovActionAppeal(ctx context.Context, actionType reptypes.GovActionType, actionTarget string, appellant sdk.AccAddress, reason string) (uint64, uint64, error)

	// Content conviction staking & author bonds
	GetContentConviction(ctx context.Context, targetType reptypes.StakeTargetType, targetID uint64) (math.LegacyDec, error)
	CreateAuthorBond(ctx context.Context, author sdk.AccAddress, targetType reptypes.StakeTargetType, targetID uint64, amount math.Int) (uint64, error)
	SlashAuthorBond(ctx context.Context, targetType reptypes.StakeTargetType, targetID uint64) error

	// GetAuthorBond returns the author bond stake for a content item. Used by
	// MsgHidePost to snapshot the bond amount before slashing, so it can be
	// restored on reversal. Returns ErrAuthorBondNotFound (or similar) when
	// no bond is attached — callers should treat that as "nothing to record".
	GetAuthorBond(ctx context.Context, targetType reptypes.StakeTargetType, targetID uint64) (reptypes.Stake, error)

	// RestoreAuthorBond is the inverse of SlashAuthorBond used by reversal
	// paths (MsgUnhidePost / appeal-OVERTURNED). Mints `amount` DREAM to
	// `author` and re-locks it as a fresh author bond on `targetID`. The
	// round-trip (slash → restore) is net-zero on DREAM supply. No-op if a
	// bond already exists for the target (idempotent against accidental
	// double-restore).
	RestoreAuthorBond(ctx context.Context, author sdk.AccAddress, targetType reptypes.StakeTargetType, targetID uint64, amount math.Int) error

	// Initiative reference validation and linking for conviction propagation
	ValidateInitiativeReference(ctx context.Context, initiativeID uint64) error
	RegisterContentInitiativeLink(ctx context.Context, initiativeID uint64, targetType int32, targetID uint64) error
	RemoveContentInitiativeLink(ctx context.Context, initiativeID uint64, targetType int32, targetID uint64) error

	// Tag registry (owned by x/rep)
	TagExists(ctx context.Context, name string) (bool, error)
	IsReservedTag(ctx context.Context, name string) (bool, error)
	GetTag(ctx context.Context, name string) (reptypes.Tag, error)
	IncrementTagUsage(ctx context.Context, name string, timestamp int64) error
	DecrementTagUsage(ctx context.Context, name string) error
	SetReservedTag(ctx context.Context, rt reptypes.ReservedTag) error

	// Salvation counters (stored on rep's Member record)
	GetSalvationCounters(ctx context.Context, addr string) (epochSalvations uint32, lastEpoch int64, err error)
	UpdateSalvationCounters(ctx context.Context, addr string, epochSalvations uint32, lastEpoch int64) error

	// Bonded-role accountability (owned by x/rep).
	// Sentinel is the forum role keyed as ROLE_TYPE_FORUM_SENTINEL.
	GetBondedRole(ctx context.Context, roleType reptypes.RoleType, addr string) (reptypes.BondedRole, error)
	ReserveBond(ctx context.Context, roleType reptypes.RoleType, addr string, amount math.Int) error
	ReleaseBond(ctx context.Context, roleType reptypes.RoleType, addr string, amount math.Int) error
	RecordActivity(ctx context.Context, roleType reptypes.RoleType, addr string) error
	SetBondStatus(ctx context.Context, roleType reptypes.RoleType, addr string, status reptypes.BondedRoleStatus, cooldownUntil int64) error
	SetBondedRoleConfig(ctx context.Context, cfg reptypes.BondedRoleConfig) error

	// Reputation slash (per-tag). Called from ExpireHiddenPosts to deduct
	// reputation from a post's author for each tag when an unappealed
	// sentinel hide finalizes. Floors at zero; no-op if the member has no
	// rep in the tag.
	DeductReputation(ctx context.Context, memberAddr sdk.AccAddress, tag string, amount math.LegacyDec) error

	// Forum-earned reputation (per-tag). Counted toward PROVISIONAL and
	// ESTABLISHED trust thresholds in x/rep but excluded from TRUSTED and
	// CORE — discourse is too cheap to game to gate council-adjacent tiers.
	// Used by the PostConvictionStake accrual loop and ExpireHiddenPosts
	// slash path.
	AddForumRep(ctx context.Context, memberAddr sdk.AccAddress, tag string, amount math.LegacyDec) error
	DeductForumRep(ctx context.Context, memberAddr sdk.AccAddress, tag string, amount math.LegacyDec) error

	// Member-accountability warnings. Called from ExpireHiddenPosts to
	// record a MemberWarning against a post's promoter (who called
	// MsgMakePostPermanent) when the promoted content is hidden unappealed.
	// `issuedBy` should be the forum module address so the warning's origin
	// is auditable. `reason` is a stable short identifier; `evidencePostIDs`
	// lists the offending post(s).
	IssueWarning(ctx context.Context, member string, issuedBy string, reason string, evidencePostIDs []uint64) error
}

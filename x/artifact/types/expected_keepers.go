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
	GetBalance(ctx context.Context, addr sdk.AccAddress, denom string) sdk.Coin
	SendCoins(ctx context.Context, fromAddr sdk.AccAddress, toAddr sdk.AccAddress, amt sdk.Coins) error
	SendCoinsFromAccountToModule(ctx context.Context, senderAddr sdk.AccAddress, recipientModule string, amt sdk.Coins) error
	SendCoinsFromModuleToAccount(ctx context.Context, senderModule string, recipientAddr sdk.AccAddress, amt sdk.Coins) error
	BurnCoins(ctx context.Context, moduleName string, amt sdk.Coins) error
	BlockedAddr(addr sdk.AccAddress) bool
}

// IdentityKeeper resolves the chain's denoms. Late-wired from app.go.
type IdentityKeeper interface {
	BondDenom(ctx context.Context) string
	DreamDenom(ctx context.Context) string
}

// RepKeeper is the subset of x/rep used for membership gates and sentinel
// moderation. Late-wired from app.go.
type RepKeeper interface {
	IsActiveMember(ctx context.Context, addr sdk.AccAddress) bool
	GetTrustLevel(ctx context.Context, addr sdk.AccAddress) (reptypes.TrustLevel, error)

	EligibleForRole(ctx context.Context, roleType reptypes.RoleType, addr string) (reptypes.BondedRole, error)
	RoleOverturnCooldownUntil(ctx context.Context, roleType reptypes.RoleType, addr string) int64
	GetAvailableBond(ctx context.Context, roleType reptypes.RoleType, addr string) (math.Int, error)
	ReserveBond(ctx context.Context, roleType reptypes.RoleType, addr string, amount math.Int) error
	ReleaseBond(ctx context.Context, roleType reptypes.RoleType, addr string, amount math.Int) error
	SlashBond(ctx context.Context, roleType reptypes.RoleType, addr string, amount math.Int, reason string) error
	RecordRoleAction(ctx context.Context, roleType reptypes.RoleType, addr, kind string) error
	RecordRoleOutcome(ctx context.Context, roleType reptypes.RoleType, addr, kind string, upheld bool) error

	// CreateGovActionAppeal opens a moderation appeal (bond, jury, deadline)
	// whose verdict comes back through RepAppealTarget.
	CreateGovActionAppeal(ctx context.Context, actionType reptypes.GovActionType, actionTarget string, appellant sdk.AccAddress, reason string) (uint64, uint64, error)

	// GetInitiative reads an initiative an impact certificate claims
	// (docs/x-artifact-spec.md §7.21). Read-only: x/rep never calls back.
	GetInitiative(ctx context.Context, initiativeID uint64) (reptypes.Initiative, error)

	// Member key migration alias contract (docs/x-rep-spec.md, "Member key
	// migration"). Stored addresses are interpreted through these; artifact
	// never re-keys its records.
	CanActAs(ctx context.Context, signer, stored string) bool
	SameMember(ctx context.Context, a, b string) bool
	RecentAddressesOf(ctx context.Context, member string) []string
	AllAddressesOf(ctx context.Context, member string) []string
}

// SessionKeeper is the slice of x/session Harberger taxes use
// (docs/x-artifact-spec.md §7.24). Late-wired from app.go; nil means no
// Harberger token can name a tax grant.
type SessionKeeper interface {
	// RecurringPull describes a RecurringPull grant, or found == false.
	RecurringPull(ctx context.Context, grantID uint64) (RecurringPullInfo, bool, error)
	// ClaimRecurringPull claims one due period of a RecurringPull granted
	// to the artifact module and returns what was pulled.
	ClaimRecurringPull(ctx context.Context, grantID uint64) (sdk.Coin, error)
}

// SeasonKeeper is the slice of x/season renderer state v2 reads
// (docs/x-artifact-spec.md §7.16). Late-wired from app.go; nil leaves the
// season fields zero.
type SeasonKeeper interface {
	// CurrentSeason returns the current season's number and status.
	CurrentSeason(ctx context.Context) (SeasonInfo, error)
	// MemberSeason returns a member's level and XP this season, or found
	// == false without a profile.
	MemberSeason(ctx context.Context, addr string) (MemberSeason, bool, error)
}

// RecurringPullInfo is what Harberger taxes need to know about a grant.
type RecurringPullInfo struct {
	Granter, Grantee string
	// Active is false for a paused, revoked, completed or expired grant.
	Active bool
	Amount sdk.Coin
	// PeriodSeconds between pulls; NextClaim is when the next one is due.
	PeriodSeconds, NextClaim, ExpiresAt int64
}

// CommonsKeeper is the subset of x/commons used for authorization.
// Late-wired from app.go.
type CommonsKeeper interface {
	// IsCouncilAuthorized accepts gov, the council policy, the committee
	// policy, or any individual committee member.
	IsCouncilAuthorized(ctx context.Context, addr string, council string, committee string) bool
	// IsCouncilBodyPolicy reports whether addr is the gov authority or the
	// policy address of a council or one of its standing committees.
	IsCouncilBodyPolicy(ctx context.Context, addr string) bool
}

// DistrKeeper routes sale fees to the community pool. Late-wired from app.go.
type DistrKeeper interface {
	FundCommunityPool(ctx context.Context, amount sdk.Coins, sender sdk.AccAddress) error
}

// ParamSubspace defines the expected Subspace interface for parameters.
type ParamSubspace interface {
	Get(context.Context, []byte, interface{})
	Set(context.Context, []byte, interface{})
}

// GnoVMKeeper is the slice of x/gnovm renderers use (docs/x-artifact-spec.md
// §7.16). Late-wired from app.go; nil means renderers are refused at class
// creation and never evaluated.
type GnoVMKeeper interface {
	// Package returns the stored source and exported function signatures of
	// pkgPath, or found == false.
	Package(ctx context.Context, pkgPath string) (GnoPackage, bool, error)
	// EvalString evaluates expr in pkgPath read-only, under x/gnovm's query
	// gas and allocation caps, and returns its single string result. A panic
	// or out-of-gas inside the VM comes back as an error.
	EvalString(ctx context.Context, pkgPath, expr string) (string, error)
}

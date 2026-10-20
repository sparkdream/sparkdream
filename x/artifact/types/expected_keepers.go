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

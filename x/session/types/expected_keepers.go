package types

import (
	"context"

	"cosmossdk.io/core/address"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// AuthKeeper defines the expected interface for the Auth module.
type AuthKeeper interface {
	AddressCodec() address.Codec
	GetAccount(context.Context, sdk.AccAddress) sdk.AccountI
	GetModuleAddress(name string) sdk.AccAddress
}

// BankKeeper defines the expected interface for the Bank module.
type BankKeeper interface {
	SpendableCoins(context.Context, sdk.AccAddress) sdk.Coins
	SendCoins(ctx context.Context, fromAddr, toAddr sdk.AccAddress, amt sdk.Coins) error
	SendCoinsFromAccountToModule(ctx context.Context, senderAddr sdk.AccAddress, recipientModule string, amt sdk.Coins) error
	SendCoinsFromModuleToAccount(ctx context.Context, senderModule string, recipientAddr sdk.AccAddress, amt sdk.Coins) error
	SendCoinsFromModuleToModule(ctx context.Context, senderModule, recipientModule string, amt sdk.Coins) error
	// BlockedAddr reports whether addr is a blocked module account
	// (bank's SendCoins does not check it, so session does).
	BlockedAddr(addr sdk.AccAddress) bool
}

// CommonsKeeper defines the expected interface for the Commons module.
// Used for council-gated operational parameter updates. Optional — nil falls
// back to governance authority only.
type CommonsKeeper interface {
	IsCouncilAuthorized(ctx context.Context, addr string, council string, committee string) bool
	// IsCouncilOrCommitteePolicy is true only for the gov authority or a
	// council / committee policy address (an executed proposal), never a
	// single member's signature.
	IsCouncilOrCommitteePolicy(ctx context.Context, addr string, council string, committee string) bool
}

// GranterOwner is implemented by a module that uses the module bypass
// (CreateGrantOnBehalfOf / RevokeGrantInternal) for granters it controls.
// Registered per calling module address from app.go (SetGranterOwner).
// Without one, a module can only revoke grants it created itself and cannot
// create grants on anyone's behalf.
type GranterOwner interface {
	// ManagesGranter reports whether the module controls granter. revoke is
	// true for a revocation, which may also cover a granter the module
	// controlled before (e.g. a deleted council policy).
	ManagesGranter(ctx context.Context, granter string, revoke bool) bool
}

// IdentityKeeper is the subset of x/identity that session reads to resolve
// the chain's bond and dream denoms at runtime. Late-bound via
// SetIdentityKeeper from app.go. Required: session panics on first denom
// lookup if identity isn't wired (no silent fallback to a hardcoded literal).
type IdentityKeeper interface {
	IsIdentityKeeper() // marker — disambiguates from rep/session.Keeper for depinject
	BondDenom(ctx context.Context) string
	DreamDenom(ctx context.Context) string
}

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
}

// CommonsKeeper defines the expected interface for the Commons module.
// Used for council-gated operational parameter updates. Optional — nil falls
// back to governance authority only.
type CommonsKeeper interface {
	IsCouncilAuthorized(ctx context.Context, addr string, council string, committee string) bool
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

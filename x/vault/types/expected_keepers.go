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
	NewAccountWithAddress(context.Context, sdk.AccAddress) sdk.AccountI
	SetAccount(context.Context, sdk.AccountI)
}

// BankKeeper defines the expected interface for the Bank module.
type BankKeeper interface {
	SpendableCoins(context.Context, sdk.AccAddress) sdk.Coins
	SendCoinsFromAccountToModule(ctx context.Context, senderAddr sdk.AccAddress, recipientModule string, amt sdk.Coins) error
	BurnCoins(ctx context.Context, moduleName string, amt sdk.Coins) error
}

// ParamSubspace defines the expected Subspace interface for parameters.
type ParamSubspace interface {
	Get(context.Context, []byte, interface{})
	Set(context.Context, []byte, interface{})
}

// IdentityKeeper resolves the chain's bond denom. Late-wired from app.go.
type IdentityKeeper interface {
	BondDenom(ctx context.Context) string
}

// RepKeeper is the x/rep alias contract (member-key-migration plan §6) plus
// the personal-address check. Late-wired from app.go; nil in unit tests, where
// every helper degrades to plain equality.
type RepKeeper interface {
	// CanActAs reports whether signer may act for the stored address (same
	// member after a key migration; a tombstoned signer never passes).
	CanActAs(ctx context.Context, signer, stored string) bool
	// Canonical returns the current address of a possibly migrated address.
	Canonical(ctx context.Context, addr string) string
	// AllAddressesOf lists every address a member has used.
	AllAddressesOf(ctx context.Context, member string) []string
	// IsPersonalAddress fails for module accounts, module-derived addresses,
	// group policies, reserved addresses and vaults.
	IsPersonalAddress(ctx context.Context, addr string) error
	// IsTombstoned reports whether addr has migrated away.
	IsTombstoned(ctx context.Context, addr string) bool
}

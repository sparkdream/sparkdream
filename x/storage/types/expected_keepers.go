package types

import (
	"context"

	"cosmossdk.io/core/address"
	sdk "github.com/cosmos/cosmos-sdk/types"

	reptypes "sparkdream/x/rep/types"
	servicetypes "sparkdream/x/service/types"
)

// AuthKeeper defines the expected interface for the Auth module.
type AuthKeeper interface {
	AddressCodec() address.Codec
	GetAccount(context.Context, sdk.AccAddress) sdk.AccountI // only used for simulation
	GetModuleAddress(moduleName string) sdk.AccAddress
}

// BankKeeper defines the expected interface for the Bank module.
type BankKeeper interface {
	SpendableCoins(context.Context, sdk.AccAddress) sdk.Coins
	GetBalance(ctx context.Context, addr sdk.AccAddress, denom string) sdk.Coin
	SendCoinsFromAccountToModule(ctx context.Context, senderAddr sdk.AccAddress, recipientModule string, amt sdk.Coins) error
	SendCoinsFromModuleToAccount(ctx context.Context, senderModule string, recipientAddr sdk.AccAddress, amt sdk.Coins) error
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

// RepKeeper is membership, trust level and the alias contract for migrated
// members. Late-wired from app.go.
type RepKeeper interface {
	IsActiveMember(ctx context.Context, addr sdk.AccAddress) bool
	GetTrustLevel(ctx context.Context, addr sdk.AccAddress) (reptypes.TrustLevel, error)
	// CanActAs reports whether signer may act for the stored address.
	CanActAs(ctx context.Context, signer, stored string) bool
	// Canonical returns the current address of a possibly migrated address.
	Canonical(ctx context.Context, addr string) string
	// SameMember reports whether two addresses belong to one member.
	SameMember(ctx context.Context, a, b string) bool
	// RecentAddressesOf lists a member's recent addresses in one read.
	RecentAddressesOf(ctx context.Context, member string) []string
	// AllAddressesOf lists every address a member has used.
	AllAddressesOf(ctx context.Context, member string) []string
}

// ServiceKeeper is the slice of x/service this module reads. It registers no
// hooks: status is read at each step. Late-wired from app.go.
type ServiceKeeper interface {
	// GetOperator returns the live record; retired and slashed operators
	// are archived and not found here.
	GetOperator(ctx context.Context, addrBytes []byte, serviceType string) (servicetypes.Operator, bool)
	// IsActiveOperator: live, ACTIVE and bonded at or above the minimum.
	IsActiveOperator(ctx context.Context, addr sdk.AccAddress, serviceType string) bool
	// HasSlashedRecord reads the archive.
	HasSlashedRecord(ctx context.Context, addrBytes []byte, serviceType string) (bool, error)
	GetServiceTypeConfig(ctx context.Context, serviceType string) (servicetypes.ServiceTypeConfig, bool)
	// UnbondingPeriodBlocks is the type's effective unbonding period.
	UnbondingPeriodBlocks(ctx context.Context, serviceType string) (int64, bool)
	OpenSystemReport(ctx context.Context, callerModuleAddr, operatorAddr sdk.AccAddress, serviceType string, slashBps uint32, evidenceURI string, dedupeKey []byte) (uint64, bool, error)
}

// CommonsKeeper answers whether an address is a council and whether a signer
// may set prices. Late-wired from app.go.
type CommonsKeeper interface {
	IsGroupPolicyAddress(ctx context.Context, addr string) bool
	IsCouncilAuthorized(ctx context.Context, addr string, council string, committee string) bool
}

// DistrKeeper funds the community pool: the fallback for a payout or refund
// that cannot be delivered. Late-wired from app.go.
type DistrKeeper interface {
	FundCommunityPool(ctx context.Context, amount sdk.Coins, sender sdk.AccAddress) error
}

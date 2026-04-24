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
	GetAccount(context.Context, sdk.AccAddress) sdk.AccountI
	GetModuleAddress(name string) sdk.AccAddress
}

// BankKeeper defines the expected interface for the Bank module.
type BankKeeper interface {
	SpendableCoins(context.Context, sdk.AccAddress) sdk.Coins
	SendCoins(ctx context.Context, fromAddr, toAddr sdk.AccAddress, amt sdk.Coins) error
	SendCoinsFromAccountToModule(ctx context.Context, senderAddr sdk.AccAddress, recipientModule string, amt sdk.Coins) error
	SendCoinsFromModuleToAccount(ctx context.Context, senderModule string, recipientAddr sdk.AccAddress, amt sdk.Coins) error
	BurnCoins(ctx context.Context, moduleName string, amt sdk.Coins) error
}

// CommonsKeeper defines the expected interface for the Commons module.
type CommonsKeeper interface {
	IsCouncilAuthorized(ctx context.Context, addr string, council string, committee string) bool
}

// RepKeeper defines the expected interface for the Reputation module.
// Methods are aligned with the actual x/rep keeper implementation.
type RepKeeper interface {
	// GetTrustLevel returns the trust level for a member.
	GetTrustLevel(ctx context.Context, addr sdk.AccAddress) (reptypes.TrustLevel, error)
	// BurnDREAM burns DREAM from an address (used for challenge-fee burns).
	BurnDREAM(ctx context.Context, addr sdk.AccAddress, amount math.Int) error
	// LockDREAM locks DREAM tokens (moves from available balance to staked).
	LockDREAM(ctx context.Context, addr sdk.AccAddress, amount math.Int) error
	// UnlockDREAM unlocks DREAM tokens (moves from staked to available balance).
	UnlockDREAM(ctx context.Context, addr sdk.AccAddress, amount math.Int) error

	// Bonded-role accountability (Phase 4). Federation verifier is the
	// module role keyed as ROLE_TYPE_FEDERATION_VERIFIER.
	GetBondedRole(ctx context.Context, roleType reptypes.RoleType, addr string) (reptypes.BondedRole, error)
	ReserveBond(ctx context.Context, roleType reptypes.RoleType, addr string, amount math.Int) error
	ReleaseBond(ctx context.Context, roleType reptypes.RoleType, addr string, amount math.Int) error
	SlashBond(ctx context.Context, roleType reptypes.RoleType, addr string, amount math.Int, reason string) error
	RecordActivity(ctx context.Context, roleType reptypes.RoleType, addr string) error
	SetBondStatus(ctx context.Context, roleType reptypes.RoleType, addr string, status reptypes.BondedRoleStatus, cooldownUntil int64) error
	SetBondedRoleConfig(ctx context.Context, cfg reptypes.BondedRoleConfig) error
}

// NameKeeper defines the expected interface for the Name module.
// Minimal interface — name resolution for outbound content creator attribution.
type NameKeeper interface {
	// GetNameOwner returns the owner of a name, if it exists.
	GetNameOwner(ctx context.Context, name string) (sdk.AccAddress, bool)
}

// Note: x/federation does not call x/shield directly. Instead, x/shield
// dispatches MsgSubmitArbiterHash to x/federation after ZK proof verification.
// Federation implements ShieldAware (see keeper/shield_aware.go) to opt in.

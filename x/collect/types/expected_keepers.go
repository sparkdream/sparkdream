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
}

// CommonsKeeper defines the expected interface for the x/commons module.
// Used for council authorization on operational parameter updates.
type CommonsKeeper interface {
	// IsCouncilAuthorized checks if an address is authorized via council/committee
	// Accepts x/gov authority, Commons Council policy address, or Operations Committee member
	IsCouncilAuthorized(ctx context.Context, addr string, council string, committee string) bool
}

// ForumKeeper defines the expected interface for the x/forum module.
// Used for sentinel bond operations in content moderation (MsgHideContent).
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
}

// ParamSubspace defines the expected Subspace interface for parameters.
type ParamSubspace interface {
	Get(context.Context, []byte, interface{})
	Set(context.Context, []byte, interface{})
}

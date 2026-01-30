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
	// Methods imported from account should be defined here
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
	SlashReputation(ctx context.Context, memberAddr sdk.AccAddress, penaltyRate math.LegacyDec, tags []string, reason string) error
}

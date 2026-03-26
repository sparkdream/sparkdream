package types

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
)

// AccountKeeper defines the expected interface for the Auth module.
type AccountKeeper interface {
	GetModuleAddress(moduleName string) sdk.AccAddress
	GetModuleAccount(ctx context.Context, moduleName string) sdk.ModuleAccountI
	GetAccount(ctx context.Context, addr sdk.AccAddress) sdk.AccountI // used by simulation
}

// BankKeeper defines the expected interface for the Bank module.
type BankKeeper interface {
	GetBalance(ctx context.Context, addr sdk.AccAddress, denom string) sdk.Coin
	SpendableCoins(ctx context.Context, addr sdk.AccAddress) sdk.Coins
	SendCoinsFromModuleToModule(ctx context.Context, senderModule, recipientModule string, amt sdk.Coins) error
}

// DistrKeeper defines the expected interface for the Distribution module.
type DistrKeeper interface {
	DistributeFromFeePool(ctx context.Context, amount sdk.Coins, receiveAddr sdk.AccAddress) error
	GetCommunityPool(ctx context.Context) (sdk.DecCoins, error)
}

// RepKeeper defines the expected interface for the Reputation module.
// Provides trust tree Merkle root snapshots for ZK proof root validation.
type RepKeeper interface {
	GetTrustTreeRoot(ctx context.Context) ([]byte, error)
	GetPreviousTrustTreeRoot(ctx context.Context) ([]byte, error)
}

// SlashingKeeper defines the expected interface for the Slashing module.
// Used for TLE liveness jailing.
type SlashingKeeper interface {
	Jail(ctx context.Context, consAddr sdk.ConsAddress) error
}

// StakingKeeper defines the expected interface for the Staking module.
// Used for TLE liveness tracking and DKG validator set snapshotting.
type StakingKeeper interface {
	GetValidator(ctx context.Context, addr sdk.ValAddress) (stakingtypes.Validator, error)
	GetValidatorByConsAddr(ctx context.Context, consAddr sdk.ConsAddress) (stakingtypes.Validator, error)
	GetBondedValidatorsByPower(ctx context.Context) ([]stakingtypes.Validator, error)
}

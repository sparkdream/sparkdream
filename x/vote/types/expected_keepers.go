package types

import (
	"context"

	"cosmossdk.io/core/address"
	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
)

// AuthKeeper defines the expected interface for the Auth module.
type AuthKeeper interface {
	AddressCodec() address.Codec
	GetAccount(context.Context, sdk.AccAddress) sdk.AccountI // only used for simulation
}

// BankKeeper defines the expected interface for the Bank module.
type BankKeeper interface {
	SpendableCoins(context.Context, sdk.AccAddress) sdk.Coins
}

// RepKeeper defines the expected interface for the x/rep module.
type RepKeeper interface {
	IsMember(ctx context.Context, member sdk.AccAddress) bool
	// MarkMemberDirty flags a member's trust-tree leaf for rebuild on the next EndBlock.
	MarkMemberDirty(ctx context.Context, address string)
}

// SeasonKeeper defines the expected interface for the x/season module.
type SeasonKeeper interface {
	GetCurrentEpoch(ctx context.Context) int64
}

// StakingKeeper defines the expected interface for the Staking module.
type StakingKeeper interface {
	GetValidator(ctx context.Context, addr sdk.ValAddress) (stakingtypes.Validator, error)
	Jail(ctx context.Context, consAddr sdk.ConsAddress) error
}

// ParamSubspace defines the expected Subspace interface for parameters.
type ParamSubspace interface {
	Get(context.Context, []byte, interface{})
	Set(context.Context, []byte, interface{})
}

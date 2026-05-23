package types

import (
	"context"

	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	minttypes "github.com/cosmos/cosmos-sdk/x/mint/types"
)

// BankKeeper defines the narrow interface the x/identity keeper needs to seed
// native-denom metadata at genesis. Listed here so consumers can mock it for
// tests without pulling in the full bankkeeper.Keeper surface.
type BankKeeper interface {
	SetDenomMetaData(ctx context.Context, denomMetaData banktypes.Metadata)
	GetDenomMetaData(ctx context.Context, denom string) (banktypes.Metadata, bool)
}

// StakingKeeper defines what SDKParamsAlignedInvariant reads from x/staking.
// Signature matches upstream stakingkeeper.Keeper.BondDenom for direct
// compatibility (no adapter needed).
type StakingKeeper interface {
	BondDenom(ctx context.Context) (string, error)
}

// MintKeeper defines what SDKParamsAlignedInvariant reads from x/mint. The
// mint keeper exposes denom via Params (no direct MintDenom accessor in
// upstream cosmos-sdk v0.50+).
type MintKeeper interface {
	GetParams(ctx context.Context) (minttypes.Params, error)
}

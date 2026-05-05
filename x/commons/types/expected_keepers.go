package types

import (
	"context"
	"time"

	"cosmossdk.io/collections"
	"cosmossdk.io/core/address"
	upgradetypes "cosmossdk.io/x/upgrade/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	v1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
)

// AuthKeeper defines the expected interface for the Auth module.
type AuthKeeper interface {
	AddressCodec() address.Codec
	GetAccount(context.Context, sdk.AccAddress) sdk.AccountI
	GetModuleAddress(string) sdk.AccAddress
	IterateAccounts(context.Context, func(sdk.AccountI) bool)
}

// BankKeeper defines the expected interface for the Bank module.
type BankKeeper interface {
	SpendableCoins(context.Context, sdk.AccAddress) sdk.Coins
	GetAllBalances(context.Context, sdk.AccAddress) sdk.Coins
	MintCoins(context.Context, string, sdk.Coins) error
	SendCoins(context.Context, sdk.AccAddress, sdk.AccAddress, sdk.Coins) error
	SendCoinsFromAccountToModule(context.Context, sdk.AccAddress, string, sdk.Coins) error
	SendCoinsFromModuleToAccount(context.Context, string, sdk.AccAddress, sdk.Coins) error
}

// GovKeeper defines the expected interface for the x/gov module.
// x/gov is a core module that stays in the chain. This interface replaces the
// concrete *govkeeper.Keeper dependency so x/commons doesn't import the gov keeper package.
type GovKeeper interface {
	GetProposal(ctx context.Context, proposalID uint64) (v1.Proposal, error)
	SetProposal(ctx context.Context, proposal v1.Proposal) error
	Tally(ctx context.Context, proposal v1.Proposal) (bool, bool, v1.TallyResult, error)
	CancelProposal(ctx context.Context, proposalID uint64, proposer string) error
	ChargeDeposit(ctx context.Context, proposalID uint64, destAddress string, percent string) error
	// Queue management
	ActiveProposalsQueueRemove(ctx context.Context, proposalID uint64, votingEndTime time.Time) error
	VotingPeriodProposalsRemove(ctx context.Context, proposalID uint64) error
}

// FutarchyKeeper defines the expected interface for the FutarchyKeeper module.
type FutarchyKeeper interface {
	CreateMarketInternal(sdk.Context, sdk.AccAddress, string, string, int64, int64, sdk.Coin) (uint64, error)
}

// SplitKeeper defines the expected interface for the Split module.
type SplitKeeper interface {
	SetShareByAddress(context.Context, string, uint64)
}

// UpgradeKeeper defines the expected interface for the Upgrade module.
type UpgradeKeeper interface {
	ScheduleUpgrade(context.Context, upgradetypes.Plan) error
}

// NameKeeper is a narrow surface used by genesis bootstrap to seed
// human-readable display names on OwnerInfo records and to register
// canonical handles for founding members.
//
// Wired post-depinject via Keeper.SetNameKeeper to avoid a depinject cycle
// (x/name already depends on x/commons via its CommonsKeeper interface).
type NameKeeper interface {
	SetDisplayName(ctx context.Context, addr string, displayName string) error
	// ClaimName atomically registers a name on behalf of a specific owner.
	// Skips fees and the membership gate (intended for cross-module / genesis
	// programmatic registration). Enforces format, blocked-names, taken,
	// and the per-address name cap.
	ClaimName(ctx context.Context, name string, owner string, data string) error
	// SetPrimaryName sets the address's primary name for reverse resolution.
	// The name must already be owned by addr (callers must claim first).
	SetPrimaryName(ctx context.Context, addr sdk.AccAddress, name string) error
}

// ParamSubspace defines the expected Subspace interface for parameters.
type ParamSubspace interface {
	Get(context.Context, []byte, interface{})
	Set(context.Context, []byte, interface{})
}

// Ensure collections.ErrNotFound is usable
var _ = collections.ErrNotFound

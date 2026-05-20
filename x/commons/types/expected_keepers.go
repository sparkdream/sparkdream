package types

import (
	"context"
	"time"

	"cosmossdk.io/collections"
	"cosmossdk.io/core/address"
	upgradetypes "cosmossdk.io/x/upgrade/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	v1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"

	sessiontypes "sparkdream/x/session/types"
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

// ForumKeeper is a narrow surface used by MsgDeleteCategory to refuse
// removing a category that still has posts attached. Wired post-depinject
// via Keeper.SetForumKeeper to avoid a depinject cycle (x/forum already
// depends on x/commons via its commonsKeeper.GetCategory call).
type ForumKeeper interface {
	HasPostInCategory(ctx context.Context, categoryID uint64) (bool, error)
}

// SessionKeeper is the narrow surface from x/session that x/commons
// consumes to host council recurring spends in the unified grant
// registry.
//
// All methods are gated on the session side by
// `params.authorized_grant_creators`; x/commons obtains the bypass by
// being seeded into that allowlist at genesis.
//
// Wired via Keeper.SetSessionKeeper post-depinject, mirroring the
// late-binding pattern used for GovKeeper / ForumKeeper.
type SessionKeeper interface {
	// P8-foundation surface (already shipped).
	CreateGrantOnBehalfOf(ctx context.Context, callerModuleAddr string, msg *sessiontypes.MsgCreateGrant) (uint64, error)
	RevokeGrantInternal(ctx context.Context, callerModuleAddr string, grantID uint64) (sdk.Coin, error)

	// Read-side helpers (M2 additions).
	GetGrant(ctx context.Context, id uint64) (sessiontypes.Grant, error)
	ListGrantsByGranter(ctx context.Context, granter string, filterType sessiontypes.GrantType) ([]sessiontypes.Grant, error)
	ListGrantsByGrantee(ctx context.Context, grantee string, filterType sessiontypes.GrantType) ([]sessiontypes.Grant, error)

	// Privileged decline + claim helpers (M2 additions). Required by
	// the D3.a wrappers in M7 and M8.
	DeclineGrantInternal(ctx context.Context, callerModuleAddr string, grantID uint64, grantee string) (sdk.Coin, error)
	ClaimRecurringPullForGrantee(ctx context.Context, callerModuleAddr string, grantID uint64, grantee string) (*sessiontypes.MsgClaimRecurringPullResponse, error)

	// SetClaimHooks registers x/commons's SessionClaimHook into the
	// session keeper. Called from app.go post-depinject (the late-
	// binding pattern).
	SetClaimHooks(hooks ...sessiontypes.GrantClaimHook)
}

// ParamSubspace defines the expected Subspace interface for parameters.
type ParamSubspace interface {
	Get(context.Context, []byte, interface{})
	Set(context.Context, []byte, interface{})
}

// Ensure collections.ErrNotFound is usable
var _ = collections.ErrNotFound

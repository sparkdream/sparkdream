package types

import (
	"context"

	"cosmossdk.io/core/address"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// SeasonState is a minimal representation of season data needed by x/rep.
// Defined here (instead of importing seasontypes.Season) to break the
// import cycle: rep/types → season/types → rep/types.
type SeasonState struct {
	Number uint64
}

// AuthKeeper defines the expected interface for the Auth module.
type AuthKeeper interface {
	AddressCodec() address.Codec
	GetAccount(context.Context, sdk.AccAddress) sdk.AccountI // only used for simulation
	// Methods imported from account should be defined here
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

// CommonsKeeper defines the expected interface for the Commons module.
type CommonsKeeper interface {
	// Check if an address is a member of a specific committee in a council
	IsCommitteeMember(ctx context.Context, address sdk.AccAddress, council string, committee string) (bool, error)

	// Get the group info for a committee
	GetCommitteeGroupInfo(ctx context.Context, council string, committee string) (interface{}, error)

	// IsCouncilAuthorized checks if addr is authorized via governance, council policy,
	// or committee membership.
	IsCouncilAuthorized(ctx context.Context, addr string, council string, committee string) bool

	// IsGroupPolicyMember checks if an address is a member of a group via its
	// policy address. Used by tag-budget operations where the budget is owned
	// by a group (council/committee) and individual messages must be signed by
	// an accountable member.
	IsGroupPolicyMember(ctx context.Context, policyAddr string, memberAddr string) (bool, error)

	// IsGroupPolicyAddress checks if the given address is a valid group policy
	// account. Used by tag-budget creation to ensure only group accounts can
	// escrow funds.
	IsGroupPolicyAddress(ctx context.Context, addr string) bool
}

// SeasonKeeper defines the expected interface for the Season module.
type SeasonKeeper interface {
	// GetCurrentSeason returns the current season state as a SeasonState.
	// Uses SeasonState (defined above) instead of seasontypes.Season to break
	// the import cycle between rep/types and season/types.
	GetCurrentSeason(ctx context.Context) (SeasonState, error)
	// ResolveDisplayNameAppealInternal resolves a display name appeal after jury verdict
	ResolveDisplayNameAppealInternal(ctx context.Context, member string, appealSucceeded bool) error
}

// SentinelActivityCounters is a decoupled, rep-side view of the forum's
// per-sentinel action counters. Returned by ForumKeeper.GetSentinelActivityCounters
// so x/rep can evaluate reward eligibility without importing forum types.
type SentinelActivityCounters struct {
	UpheldHides          uint64
	OverturnedHides      uint64
	UpheldLocks          uint64
	OverturnedLocks      uint64
	UpheldMoves          uint64
	OverturnedMoves      uint64
	EpochHides           uint64
	EpochLocks           uint64
	EpochMoves           uint64
	EpochPins            uint64
	EpochAppealsFiled    uint64
	EpochAppealsResolved uint64
}

// ForumKeeper defines the minimal forum surface area required by x/rep's tag
// moderation and tag-budget flows. Late-wired from app.go to break the
// rep → forum cycle. Will be retired when sentinel/content-moderation state
// moves into x/rep.
type ForumKeeper interface {
	// PruneTagReferences removes the given tag from every post that references it.
	// Called after ResolveTagReport removes a tag from the registry so stale
	// references don't remain in forum content.
	PruneTagReferences(ctx context.Context, tagName string) error

	// GetPostAuthor returns the author address for a post. Used by tag-budget
	// award handling to credit the post's author with the award payout.
	GetPostAuthor(ctx context.Context, postID uint64) (string, error)

	// GetPostTags returns the tag list for a post. Used by tag-budget award
	// handling to enforce that awards can only flow to posts tagged with the
	// budget's tag.
	GetPostTags(ctx context.Context, postID uint64) ([]string, error)

	// GetActionSentinel returns the sentinel address that executed the given
	// action. Looks up forum's HideRecord / ThreadLockRecord / ThreadMoveRecord
	// keyed by actionTarget (parsed as uint64 postID / rootID). Returns empty
	// string with no error if the action record is missing (GC'd or never
	// existed) so the caller can decide to skip rather than abort.
	GetActionSentinel(ctx context.Context, actionType GovActionType, actionTarget string) (string, error)

	// RecordSentinelActionUpheld increments the sentinel's upheld_* counter
	// for the action type (hide / lock / move), increments consecutive_upheld,
	// and resets consecutive_overturns. If the sentinel cannot be resolved
	// (record GC'd), logs a warning and returns nil.
	RecordSentinelActionUpheld(ctx context.Context, actionType GovActionType, actionTarget string) error

	// RecordSentinelActionOverturned increments the sentinel's overturned_*
	// counter for the action type, increments consecutive_overturns, and
	// resets consecutive_upheld. If consecutive_overturns crosses the demotion
	// threshold, calls the rep keeper to demote the sentinel. If the sentinel
	// cannot be resolved (record GC'd), logs a warning and returns nil.
	RecordSentinelActionOverturned(ctx context.Context, actionType GovActionType, actionTarget string) error

	// GetSentinelActivityCounters loads the forum-side per-sentinel counter
	// snapshot for the given address. Returns a zero-valued struct with no
	// error when the sentinel has no forum record yet (e.g., bonded but has
	// not taken a single moderation action).
	GetSentinelActivityCounters(ctx context.Context, addr string) (SentinelActivityCounters, error)

	// ResetSentinelEpochCounters zeros the forum-side per-epoch counters
	// (epoch_hides / epoch_locks / epoch_moves / epoch_pins /
	// epoch_appeals_filed / epoch_appeals_resolved). Cumulative counters are
	// preserved. No-op when the sentinel has no forum record.
	ResetSentinelEpochCounters(ctx context.Context, addr string) error
}

package types

import (
	"context"
	"time"

	"cosmossdk.io/core/address"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"

	shieldtypes "sparkdream/x/shield/types"
)

// SeasonState is a minimal representation of season data needed by x/rep.
// Defined here (instead of importing seasontypes.Season) to break the
// import cycle: rep/types → season/types → rep/types.
type SeasonState struct {
	Number uint64
}

// IdentityKeeper is the subset of x/identity that rep reads to resolve the
// chain's bond and dream denoms at runtime. Late-bound via SetIdentityKeeper
// from app.go. Required: rep panics on first denom lookup if identity isn't
// wired — silently falling back to a hardcoded literal re-introduces the
// mixed-state bug we just removed.
type IdentityKeeper interface {
	IsIdentityKeeper() // marker — disambiguates from rep/session.Keeper for depinject
	BondDenom(ctx context.Context) string
	DreamDenom(ctx context.Context) string
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
	SendCoins(ctx context.Context, fromAddr sdk.AccAddress, toAddr sdk.AccAddress, amt sdk.Coins) error
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

	// ListCommitteeMembers returns the addresses of every member of the named
	// committee of a council. Used to assign the Commons Operations Committee
	// to a budget-backed project proposal's PROJECT_APPROVAL / BUDGET_REVIEW
	// interim.
	ListCommitteeMembers(ctx context.Context, council string, committee string) ([]string, error)

	// HasVoted reports whether voter cast a vote (any option) on the
	// proposal; true for every address on a secret ballot, whose voters
	// cannot be attributed, and false for an unknown proposal. Used to pay a
	// project's review interim only to the committee members who voted on
	// the proposal that approved the project.
	HasVoted(ctx context.Context, proposalID uint64, voter string) bool

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

	// IsCouncilOrCommitteePolicy accepts the gov authority, the council's
	// policy address or the named committee's policy address, but not an
	// individual committee member. MsgForceMigration requires the Commons HR
	// committee as a body, so it uses this rather than IsCouncilAuthorized.
	IsCouncilOrCommitteePolicy(ctx context.Context, addr string, council string, committee string) bool
}

// SeasonKeeper defines the expected interface for the Season module.
type SeasonKeeper interface {
	// GetCurrentSeason returns the current season state as a SeasonState.
	// Uses SeasonState (defined above) instead of seasontypes.Season to break
	// the import cycle between rep/types and season/types.
	GetCurrentSeason(ctx context.Context) (SeasonState, error)
	// IsInMaintenanceMode reports whether the season transition is in one of
	// its blocking phases (reputation archive/reset, XP reset). Rep refuses
	// reputation-moving messages while it is set (x-season-spec Maintenance
	// Mode).
	IsInMaintenanceMode(ctx context.Context) bool

	// Gamification hooks (x-season-spec "Hook Implementations"). Rep calls
	// them after its own state write; errors are logged, never tx-halting.
	OnMemberCreated(ctx context.Context, member sdk.AccAddress) error
	OnMemberEstablished(ctx context.Context, member sdk.AccAddress) error
	OnInitiativeCompleted(ctx context.Context, member sdk.AccAddress, initiativeID uint64) error
	OnChallengeResolved(ctx context.Context, challenger, challengee sdk.AccAddress, challengerWon bool) error
	OnJuryDutyCompleted(ctx context.Context, juror sdk.AccAddress, juryReviewID uint64) error
	OnMemberZeroed(ctx context.Context, member sdk.AccAddress) error
	// ResolveDisplayNameAppealInternal resolves a display name appeal after jury verdict
	ResolveDisplayNameAppealInternal(ctx context.Context, member string, appealSucceeded bool) error
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

	// OnSentinelActionResolved applies forum-LOCAL bookkeeping when a jury
	// verdict lands on a sentinel action (currently: decrement the
	// pending-hide count for hide verdicts). The SHARED accountability
	// consequences (streaks, ring, cooldown, demotion) are applied by x/rep
	// itself via RecordRoleOutcome — this hook only covers what x/rep cannot
	// reach. No-op (nil) when the action record is missing.
	OnSentinelActionResolved(ctx context.Context, actionType GovActionType, actionTarget string) error

	// GetActionCommittedAmount returns the bond amount that was reserved by
	// the sentinel for the given gov action. Used by the rep appeal-resolver
	// to release the exact reservation on UPHELD verdicts. Returns zero (no
	// error) when the record is missing or does not track a committed amount.
	GetActionCommittedAmount(ctx context.Context, actionType GovActionType, actionTarget string) (math.Int, error)

	// ReverseSentinelAction reverses the content-state effects of a sentinel
	// moderation action: unhides a hidden post, unlocks a locked thread, or
	// restores a moved thread to its original category. Called from the
	// appeal-OVERTURNED branch in MsgResolveGovActionAppeal so the appeal
	// loop is end-to-end complete (sentinel slashed AND content restored).
	// Bypasses user-facing auth/cooldown checks; carries its own dangling-
	// reference guard (refuses to restore a post into a deleted category).
	// Missing action records are a soft skip — caller should log and continue
	// rather than abort the transaction.
	ReverseSentinelAction(ctx context.Context, actionType GovActionType, actionTarget string) error
}

// ModerationAppealTarget is the owning-module side of a moderation appeal
// filed through CreateGovActionAppeal. x/rep holds the appellant bond, seats
// the jury, applies the verdict to the sentinel's bond and RoleActivity
// record, and calls back into the module that owns the moderated content.
// ForumKeeper satisfies it (forum's action types fall back to the wired
// forum keeper); other modules register one per action type with
// Keeper.RegisterModerationAppealTarget.
//
// GetActionSentinel must return "" (and GetActionCommittedAmount zero) once
// the module has closed the action on its own (e.g. the content was
// destroyed), so a late verdict does not release or slash a bond twice.
type ModerationAppealTarget interface {
	GetActionSentinel(ctx context.Context, actionType GovActionType, actionTarget string) (string, error)
	GetActionCommittedAmount(ctx context.Context, actionType GovActionType, actionTarget string) (math.Int, error)
	OnSentinelActionResolved(ctx context.Context, actionType GovActionType, actionTarget string) error
	ReverseSentinelAction(ctx context.Context, actionType GovActionType, actionTarget string) error
}

// ModerationAppealOutcomeHandler is optionally implemented by a
// ModerationAppealTarget that must act on every terminal appeal outcome, not
// only on OVERTURNED (which ReverseSentinelAction covers). Called after x/rep
// has applied its own bond and accountability effects:
//
//   - UPHELD:     the hide stands; the module finalizes it (e.g. deletes or
//     scrubs the content). x/rep has already released the sentinel's bond.
//   - OVERTURNED: called after ReverseSentinelAction; usually a no-op.
//   - TIMEOUT:    no verdict; x/rep blamed no one and did not touch the
//     sentinel's bond, so the module applies its own timeout policy and
//     releases the reservation.
type ModerationAppealOutcomeHandler interface {
	OnAppealOutcome(ctx context.Context, actionType GovActionType, actionTarget string, outcome GovAppealStatus) error
}

// ModuleOwnedAppealTypes are filed only by the owning module (which checks
// who may appeal and marks its own record appealed); MsgAppealGovAction
// rejects them. Forum's four types are filed through MsgAppealPost,
// MsgAppealThreadLock, MsgAppealThreadMove and MsgDisputePin: filed through
// rep they would do nothing to the forum record but still block the
// affected author's own appeal while pending (FRM-02).
var ModuleOwnedAppealTypes = map[GovActionType]bool{
	GovActionType_GOV_ACTION_TYPE_COLLECT_HIDE:  true,
	GovActionType_GOV_ACTION_TYPE_ARTIFACT_HIDE: true,
	GovActionType_GOV_ACTION_TYPE_POST_HIDE:     true,
	GovActionType_GOV_ACTION_TYPE_THREAD_LOCK:   true,
	GovActionType_GOV_ACTION_TYPE_THREAD_MOVE:   true,
	GovActionType_GOV_ACTION_TYPE_REPLY_PIN:     true,
}

// ForumAppealTypes are the module-owned types whose appeal target is the
// wired ForumKeeper when no target is registered for them.
var ForumAppealTypes = map[GovActionType]bool{
	GovActionType_GOV_ACTION_TYPE_POST_HIDE:   true,
	GovActionType_GOV_ACTION_TYPE_THREAD_LOCK: true,
	GovActionType_GOV_ACTION_TYPE_THREAD_MOVE: true,
	GovActionType_GOV_ACTION_TYPE_REPLY_PIN:   true,
}

// MemberActionAppealTypes are the rep-owned actions MsgAppealGovAction
// accepts: the actions MsgResolveMemberReport applies to a member. The
// action_target is the member's address, and only that member may appeal,
// within DefaultAppealDeadline of the resolution. Every other rep-owned type
// (TAG_REMOVAL, FORUM_PAUSE, REVIEW_REJECTION) has no on-chain action record
// to appeal against and is refused.
var MemberActionAppealTypes = map[GovActionType]bool{
	GovActionType_GOV_ACTION_TYPE_WARNING:  true,
	GovActionType_GOV_ACTION_TYPE_DEMOTION: true,
	GovActionType_GOV_ACTION_TYPE_ZEROING:  true,
}

// ModerationAppealRole is optionally implemented by a ModerationAppealTarget
// whose actions are taken by a bonded role other than the content sentinel
// (for example a curator). x/rep releases, slashes and scores that role's
// bond on the verdict. Targets that do not implement it are content
// sentinel actions.
type ModerationAppealRole interface {
	AppealRoleType(actionType GovActionType) RoleType
}

// BlogKeeper defines the minimal blog surface area required by x/rep to
// validate self-stake on blog content. Late-wired from app.go.
type BlogKeeper interface {
	// GetPostAuthor returns the author address for the given blog post.
	GetPostAuthor(ctx context.Context, postID uint64) (string, error)
	// GetReplyAuthor returns the author address for the given blog reply
	// (replies have their own id sequence: STAKE_TARGET_BLOG_REPLY_CONTENT).
	GetReplyAuthor(ctx context.Context, replyID uint64) (string, error)
}

// CollectKeeper defines the minimal collect surface area required by x/rep
// to validate self-stake on collection content. Late-wired from app.go.
type CollectKeeper interface {
	// GetCollectionOwner returns the owner address for the given collection.
	GetCollectionOwner(ctx context.Context, collectionID uint64) (string, error)
}

// DistrKeeper is the slice of x/distribution x/rep needs to auto-fund its
// bonded-role reward pools from the community pool.
//
// Late-wired in app.go rather than declared as a depinject dependency: even an
// optional dependency participates in cycle detection, and rep sits upstream of
// too much to take one safely.
type DistrKeeper interface {
	DistributeFromFeePool(ctx context.Context, amount sdk.Coins, receiveAddr sdk.AccAddress) error
	// SetWithdrawAddr points a delegator's staking rewards at another
	// address. Migration finalization sets the tombstone's withdraw address
	// to the canonical address so rewards keep reaching the member.
	SetWithdrawAddr(ctx context.Context, delegatorAddr, withdrawAddr sdk.AccAddress) error
	GetCommunityPool(ctx context.Context) (sdk.DecCoins, error)
	// GetCommunityTax is the fraction of block rewards routed to the community
	// pool. Together with annual provisions it gives the pool's income rate,
	// which is what role_reward_inflation_share is a share OF.
	GetCommunityTax(ctx context.Context) (math.LegacyDec, error)
}

// StakingKeeper is the slice of x/staking x/rep needs to unbond the
// delegations a RECOVERY or COUNCIL migration leaves at the tombstone (the old
// key is lost or stolen, so nobody can be relied on to undelegate them).
// Late-wired in app.go (SetStakingKeeper), like DistrKeeper.
type StakingKeeper interface {
	GetDelegatorDelegations(ctx context.Context, delegator sdk.AccAddress, maxRetrieve uint16) ([]stakingtypes.Delegation, error)
	Undelegate(ctx context.Context, delAddr sdk.AccAddress, valAddr sdk.ValAddress, sharesAmount math.LegacyDec) (time.Time, math.Int, error)
}

// MintKeeper is the slice of x/mint x/rep needs to size the bonded-role
// funding draw.
//
// Only annual provisions: supply x current inflation, recomputed every block by
// mint's BeginBlocker. Reading the rate rather than the community pool balance
// is deliberate — the balance includes the 95M SPARK genesis allocation and any
// direct deposit, neither of which is income. x/mint's authority is the burn
// address, so this anchors the funding rate to a number governance cannot move.
type MintKeeper interface {
	AnnualProvisions(ctx context.Context) (math.LegacyDec, error)
}

// BeaconSource is the slice of x/shield x/rep needs to seed jury draws from
// the TLE randomness beacon (docs/x-shield-spec.md "Randomness beacon").
// Late-wired in app.go (SetBeaconSource): x/shield already depends on x/rep
// through SetRepKeeper, so a depinject field here would close a cycle. Unwired
// (unit tests, or a chain without x/shield) means every draw uses the
// block-hash seed.
type BeaconSource interface {
	// CurrentEpoch is the shield epoch now open.
	CurrentEpoch(ctx context.Context) uint64
	// BeaconEpochInterval is the shield epoch length in blocks.
	BeaconEpochInterval(ctx context.Context) uint64
	// BeaconStatus reports whether epoch's beacon value is AVAILABLE, still
	// PENDING, or UNAVAILABLE (will never come).
	BeaconStatus(ctx context.Context, epoch uint64) shieldtypes.BeaconStatus
	// BeaconKey is epoch's beacon value (the canonical encoding of its TLE
	// epoch key) when AVAILABLE.
	BeaconKey(ctx context.Context, epoch uint64) ([]byte, bool)
}

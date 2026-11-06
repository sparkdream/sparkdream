package keeper

import (
	"context"
	"encoding/binary"
	"fmt"

	"sparkdream/x/commons/types"

	"cosmossdk.io/collections"
	"cosmossdk.io/core/address"
	corestore "cosmossdk.io/core/store"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkaddress "github.com/cosmos/cosmos-sdk/types/address"
)

// lateKeepers holds dependencies that are wired after depinject via Set* methods.
// Stored as a shared pointer so value-copies of Keeper (e.g. in AppModule, msgServer)
// see updates made after NewAppModule().
type lateKeepers struct {
	govKeeper      types.GovKeeper
	router         baseapp.MessageRouter
	nameKeeper     types.NameKeeper
	forumKeeper    types.ForumKeeper
	sessionKeeper  types.SessionKeeper
	identityKeeper types.IdentityKeeper
	seasonKeeper   types.SeasonKeeper
	repKeeper      types.MigrationRepKeeper
}

type Keeper struct {
	storeService corestore.KVStoreService
	cdc          codec.Codec
	addressCodec address.Codec
	// Address capable of executing a MsgUpdateParams message.
	// Typically, this should be the x/gov module account.
	authority []byte

	Schema collections.Schema
	Params collections.Item[types.Params]

	authKeeper     types.AuthKeeper
	bankKeeper     types.BankKeeper
	futarchyKeeper types.FutarchyKeeper
	splitKeeper    types.SplitKeeper
	upgradeKeeper  types.UpgradeKeeper
	late           *lateKeepers

	// --- Existing collections (unchanged) ---
	PolicyPermissions  collections.Map[string, types.PolicyPermissions]
	Groups             collections.Map[string, types.Group]
	PolicyToName       collections.Map[string, string]
	MarketToGroup      collections.Map[uint64, string]
	MarketTriggerQueue collections.KeySet[collections.Pair[int64, string]]

	// --- Native governance collections ---

	// Members stores council/committee members: (council_name, member_address) -> Member
	Members collections.Map[collections.Pair[string, string], types.Member]
	// DecisionPolicies stores voting rules per policy address: policy_address -> DecisionPolicy
	DecisionPolicies collections.Map[string, types.DecisionPolicy]
	// Proposals stores all proposals: proposal_id -> Proposal
	Proposals collections.Map[uint64, types.Proposal]
	// ProposalSeq is the auto-incrementing proposal ID sequence
	ProposalSeq collections.Sequence
	// CouncilSeq is the auto-incrementing council ID sequence
	CouncilSeq collections.Sequence
	// PolicyVersion tracks the current policy version for veto invalidation
	PolicyVersion collections.Map[string, uint64]
	// Votes stores individual votes: (proposal_id, voter_address) -> Vote
	Votes collections.Map[collections.Pair[uint64, string], types.Vote]
	// ProposalsByCouncil is an index: (council_name, proposal_id) -> empty
	ProposalsByCouncil collections.KeySet[collections.Pair[string, uint64]]
	// VetoPolicies maps a council name to its veto policy address
	VetoPolicies collections.Map[string, string]
	// AnonVoteTallies stores anonymous vote counts per proposal: proposal_id -> AnonVoteTally
	AnonVoteTallies collections.Map[uint64, types.AnonVoteTally]
	// EpochSpending tracks cumulative spending per (policy_address, epoch_day).
	// Value is the cumulative uspark amount spent in the epoch (string-encoded).
	EpochSpending collections.Map[collections.Pair[string, int64], string]
	// DeletedPolicies holds the policy addresses of deleted groups.
	DeletedPolicies collections.KeySet[string]
	// ProposalQueue indexes open proposals by the time they next need
	// EndBlock attention: (due_time, proposal_id).
	ProposalQueue collections.KeySet[collections.Pair[int64, uint64]]
	// AnonProposalCount rate-limits anonymous proposals per council per UTC
	// day: (council_name, utc_day) -> count.
	AnonProposalCount collections.Map[collections.Pair[string, int64], uint64]
	// ProposalVoterTrees holds secret-ballot voter snapshots while voting is
	// open: proposal_id -> VoterTree.
	ProposalVoterTrees collections.Map[uint64, types.VoterTree]

	// Polls (polls.go).
	Polls              collections.Map[uint64, types.Poll]
	PollSeq            collections.Sequence
	PollVotes          collections.Map[collections.Pair[uint64, string], types.PollVote]
	PollsByCouncil     collections.KeySet[collections.Pair[string, uint64]]
	OpenPollsByCouncil collections.KeySet[collections.Pair[string, uint64]]
	PollQueue          collections.KeySet[collections.Pair[int64, uint64]]
	PollVoterTrees     collections.Map[uint64, types.VoterTree]
	// PollOpenCounts rate-limits poll openings per creator per UTC day:
	// creator -> PollOpenCount (one record per creator, reset on a new day).
	PollOpenCounts collections.Map[string, types.PollOpenCount]

	Category    collections.Map[uint64, types.Category]
	CategorySeq collections.Sequence

	// Recurring-spend storage moved to session.Grants as RECURRING_PULL
	// variants; queries project on-the-fly. See query_recurring_spend.go.
}

func NewKeeper(
	storeService corestore.KVStoreService,
	cdc codec.Codec,
	addressCodec address.Codec,
	authority []byte,

	authKeeper types.AuthKeeper,
	bankKeeper types.BankKeeper,
	futarchyKeeper types.FutarchyKeeper,
	govKeeper types.GovKeeper,
	splitKeeper types.SplitKeeper,
	upgradeKeeper types.UpgradeKeeper,
) Keeper {
	if _, err := addressCodec.BytesToString(authority); err != nil {
		panic(fmt.Sprintf("invalid authority address %s: %s", authority, err))
	}

	sb := collections.NewSchemaBuilder(storeService)

	k := Keeper{
		storeService: storeService,
		cdc:          cdc,
		addressCodec: addressCodec,
		authority:    authority,

		authKeeper:     authKeeper,
		bankKeeper:     bankKeeper,
		futarchyKeeper: futarchyKeeper,
		splitKeeper:    splitKeeper,
		upgradeKeeper:  upgradeKeeper,
		late:           &lateKeepers{govKeeper: govKeeper},

		// Existing collections
		Params:            collections.NewItem(sb, types.ParamsKey, "params", codec.CollValue[types.Params](cdc)),
		PolicyPermissions: collections.NewMap(sb, types.PolicyPermissionsKey, "policyPermissions", collections.StringKey, codec.CollValue[types.PolicyPermissions](cdc)),
		Groups:            collections.NewMap(sb, types.GroupKey, "group", collections.StringKey, codec.CollValue[types.Group](cdc)),
		PolicyToName:      collections.NewMap(sb, types.PolicyToNameKey, "policyToName", collections.StringKey, collections.StringValue),
		MarketToGroup:     collections.NewMap(sb, types.MarketToGroupKey, "market_to_group", collections.Uint64Key, collections.StringValue),
		MarketTriggerQueue: collections.NewKeySet(
			sb,
			types.MarketTriggerQueueKey,
			"market_trigger_queue",
			collections.PairKeyCodec(collections.Int64Key, collections.StringKey),
		),

		// New native governance collections
		Members: collections.NewMap(
			sb, types.MembersKey, "members",
			collections.PairKeyCodec(collections.StringKey, collections.StringKey),
			codec.CollValue[types.Member](cdc),
		),
		DecisionPolicies: collections.NewMap(
			sb, types.DecisionPoliciesKey, "decisionPolicies",
			collections.StringKey,
			codec.CollValue[types.DecisionPolicy](cdc),
		),
		Proposals: collections.NewMap(
			sb, types.ProposalsKey, "proposals",
			collections.Uint64Key,
			codec.CollValue[types.Proposal](cdc),
		),
		ProposalSeq: collections.NewSequence(sb, types.ProposalSeqKey, "proposal_seq"),
		CouncilSeq:  collections.NewSequence(sb, types.CouncilSeqKey, "council_seq"),
		PolicyVersion: collections.NewMap(
			sb, types.PolicyVersionKey, "policyVersion",
			collections.StringKey,
			collections.Uint64Value,
		),
		Votes: collections.NewMap(
			sb, types.VotesKey, "votes",
			collections.PairKeyCodec(collections.Uint64Key, collections.StringKey),
			codec.CollValue[types.Vote](cdc),
		),
		ProposalsByCouncil: collections.NewKeySet(
			sb, types.ProposalsByCouncilKey, "proposalsByCouncil",
			collections.PairKeyCodec(collections.StringKey, collections.Uint64Key),
		),
		VetoPolicies: collections.NewMap(
			sb, types.VetoPoliciesKey, "vetoPolicies",
			collections.StringKey,
			collections.StringValue,
		),
		AnonVoteTallies: collections.NewMap(
			sb, types.AnonVoteTalliesKey, "anonVoteTallies",
			collections.Uint64Key,
			codec.CollValue[types.AnonVoteTally](cdc),
		),
		EpochSpending: collections.NewMap(
			sb, types.EpochSpendingKey, "epochSpending",
			collections.PairKeyCodec(collections.StringKey, collections.Int64Key),
			collections.StringValue,
		),
		DeletedPolicies: collections.NewKeySet(
			sb, types.DeletedPoliciesKey, "deletedPolicies",
			collections.StringKey,
		),
		ProposalQueue: collections.NewKeySet(
			sb, types.ProposalQueueKey, "proposalQueue",
			collections.PairKeyCodec(collections.Int64Key, collections.Uint64Key),
		),
		AnonProposalCount: collections.NewMap(
			sb, types.AnonProposalCountKey, "anonProposalCount",
			collections.PairKeyCodec(collections.StringKey, collections.Int64Key),
			collections.Uint64Value,
		),
		ProposalVoterTrees: collections.NewMap(
			sb, types.ProposalVoterTreesKey, "proposalVoterTrees",
			collections.Uint64Key, codec.CollValue[types.VoterTree](cdc),
		),
		Polls: collections.NewMap(
			sb, types.PollsKey, "polls",
			collections.Uint64Key, codec.CollValue[types.Poll](cdc),
		),
		PollSeq: collections.NewSequence(sb, types.PollSeqKey, "pollSequence"),
		PollVotes: collections.NewMap(
			sb, types.PollVotesKey, "pollVotes",
			collections.PairKeyCodec(collections.Uint64Key, collections.StringKey),
			codec.CollValue[types.PollVote](cdc),
		),
		PollsByCouncil: collections.NewKeySet(
			sb, types.PollsByCouncilKey, "pollsByCouncil",
			collections.PairKeyCodec(collections.StringKey, collections.Uint64Key),
		),
		OpenPollsByCouncil: collections.NewKeySet(
			sb, types.OpenPollsByCouncilKey, "openPollsByCouncil",
			collections.PairKeyCodec(collections.StringKey, collections.Uint64Key),
		),
		PollQueue: collections.NewKeySet(
			sb, types.PollQueueKey, "pollQueue",
			collections.PairKeyCodec(collections.Int64Key, collections.Uint64Key),
		),
		PollVoterTrees: collections.NewMap(
			sb, types.PollVoterTreesKey, "pollVoterTrees",
			collections.Uint64Key, codec.CollValue[types.VoterTree](cdc),
		),
		PollOpenCounts: collections.NewMap(
			sb, types.PollOpenCountsKey, "pollOpenCounts",
			collections.StringKey, codec.CollValue[types.PollOpenCount](cdc),
		),
		Category: collections.NewMap(
			sb, types.CategoryKey, "category",
			collections.Uint64Key,
			codec.CollValue[types.Category](cdc),
		),
		CategorySeq: collections.NewSequence(sb, types.CategorySeqKey, "categorySequence"),

		// Recurring Spend collections were removed by the M10 migration.
		// Schedules live in session.Grants as RECURRING_PULL variants.
	}

	schema, err := sb.Build()
	if err != nil {
		panic(err)
	}
	k.Schema = schema

	return k
}

// Codec returns the keeper's codec for use by ante handlers and other packages.
func (k Keeper) Codec() codec.Codec {
	return k.cdc
}

// SetGovKeeper wires the GovKeeper after depinject to break cyclic dependencies.
func (k *Keeper) SetGovKeeper(gk types.GovKeeper) {
	k.late.govKeeper = gk
}

// SetRouter wires the MsgServiceRouter after app build for proposal execution.
func (k *Keeper) SetRouter(router baseapp.MessageRouter) {
	k.late.router = router
}

// SetNameKeeper wires the NameKeeper after depinject. Used by genesis bootstrap
// to seed OwnerInfo.display_name for founding members. Late-wired to avoid a
// depinject cycle (x/name already depends on x/commons).
func (k Keeper) SetNameKeeper(nk types.NameKeeper) {
	k.late.nameKeeper = nk
}

// SetSeasonKeeper wires x/season after depinject so proposals and votes
// grant XP. Optional: unwired means no XP.
func (k Keeper) SetSeasonKeeper(sk types.SeasonKeeper) {
	k.late.seasonKeeper = sk
}

// notifySeason runs one x/season hook best-effort: the governance write has
// already happened, so a hook error is logged and swallowed.
func (k Keeper) notifySeason(ctx context.Context, hook string, fn func(sk types.SeasonKeeper) error) {
	if k.late.seasonKeeper == nil {
		return
	}
	if err := fn(k.late.seasonKeeper); err != nil {
		sdk.UnwrapSDKContext(ctx).Logger().Error("x/season hook failed (non-fatal)", "hook", hook, "error", err)
	}
}

// SetForumKeeper wires the ForumKeeper after depinject. Used by
// MsgDeleteCategory to refuse removal of categories that still have
// posts. Late-wired to avoid a depinject cycle (x/forum already depends
// on x/commons via commonsKeeper.GetCategory).
func (k Keeper) SetForumKeeper(fk types.ForumKeeper) {
	k.late.forumKeeper = fk
}

// SetIdentityKeeper late-binds the identity keeper for federated-denom
// resolution. Called from app.go post-depinject.
func (k Keeper) SetIdentityKeeper(idk types.IdentityKeeper) {
	k.late.identityKeeper = idk
}

// BondDenom returns the chain's bond denom from the wired identity keeper.
// Panics if identity isn't wired: every call site needs a real denom and
// silently falling back to a hardcoded literal would re-introduce the
// mixed-state class of bug that motivated the identity module.
func (k Keeper) BondDenom(ctx context.Context) string {
	if k.late.identityKeeper == nil {
		panic("commons keeper: identityKeeper not wired (call SetIdentityKeeper after depinject)")
	}
	return k.late.identityKeeper.BondDenom(ctx)
}

// SetSessionKeeper wires the SessionKeeper after depinject. Used by
// the RecurringSpend migration to (a) host council schedules in the
// unified grant registry via `CreateGrantOnBehalfOf` /
// `RevokeGrantInternal` / `DeclineGrantInternal` /
// `ClaimRecurringPullForGrantee`, and (b) register the
// `SessionClaimHook` via `SessionKeeper.SetClaimHooks`. Late-wired
// because x/session does not appear in x/commons's depinject inputs
// (the dependency is one-way: x/commons consumes session, never the
// reverse).
func (k Keeper) SetSessionKeeper(sk types.SessionKeeper) {
	k.late.sessionKeeper = sk
}

// SessionKeeper returns the late-wired SessionKeeper. Returns nil if
// not yet wired (which is the case during depinject; the wiring happens
// in app.go post-depinject). Callers that need it during normal tx
// handling can assume non-nil — app.go panics if SetSessionKeeper has
// not been called by the time the chain serves blocks.
func (k Keeper) SessionKeeper() types.SessionKeeper {
	return k.late.sessionKeeper
}

// DeriveCouncilAddress generates a deterministic address for a council based on its ID.
// Each (councilID, policyType) pair maps to a unique module-derived account
// (e.g. "council/standard/<id>" and "council/veto/<id>") so the policy
// address can be regenerated from genesis without persisting a separate
// allocator.
func DeriveCouncilAddress(councilID uint64, policyType string) sdk.AccAddress {
	buf := make([]byte, 8)
	binary.BigEndian.PutUint64(buf, councilID)
	key := append([]byte("council/"+policyType+"/"), buf...)
	return sdkaddress.Module(types.ModuleName, key)
}

// SetRepKeeper late-wires the slice of x/rep that caps council voting
// periods by the ZK key activation delay (member-key-migration plan,
// decision 17). Wired from app.go after depinject.
func (k Keeper) SetRepKeeper(rk types.MigrationRepKeeper) {
	k.late.repKeeper = rk
}

// canActAs is the nil-safe form of the rep alias check: the rep keeper is
// late-wired and absent in unit tests, where it degrades to equality.
func (k Keeper) canActAs(ctx context.Context, signer, stored string) bool {
	if k.late.repKeeper == nil {
		return signer == stored
	}
	return k.late.repKeeper.CanActAs(ctx, signer, stored)
}

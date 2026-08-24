package keeper

import (
	"context"
	"fmt"

	"cosmossdk.io/collections"
	"cosmossdk.io/core/address"
	corestore "cosmossdk.io/core/store"
	"github.com/cosmos/cosmos-sdk/codec"

	"sparkdream/x/rep/types"
)

// lateKeepers holds keepers that are wired after depinject initialization
// (to break cyclic dependencies). All value copies of Keeper share the same
// pointer, so mutations via SetSeasonKeeper are visible everywhere.
type lateKeepers struct {
	seasonKeeper   types.SeasonKeeper
	forumKeeper    types.ForumKeeper
	blogKeeper     types.BlogKeeper
	collectKeeper  types.CollectKeeper
	identityKeeper types.IdentityKeeper
	distrKeeper    types.DistrKeeper
	mintKeeper     types.MintKeeper
	hooks          types.RepHooks
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

	authKeeper           types.AuthKeeper
	bankKeeper           types.BankKeeper
	commonsKeeper        types.CommonsKeeper
	late                 *lateKeepers // shared across value copies
	Member               collections.Map[string, types.Member]
	InvitationSeq        collections.Sequence
	Invitation           collections.Map[uint64, types.Invitation]
	InvitationsByInvitee collections.Map[string, uint64] // invitee address -> invitation ID
	ProjectSeq           collections.Sequence
	Project              collections.Map[uint64, types.Project]
	InitiativeSeq        collections.Sequence
	Initiative           collections.Map[uint64, types.Initiative]
	StakeSeq             collections.Sequence
	Stake                collections.Map[uint64, types.Stake]
	ChallengeSeq         collections.Sequence
	Challenge            collections.Map[uint64, types.Challenge]
	JuryReviewSeq        collections.Sequence
	JuryReview           collections.Map[uint64, types.JuryReview]
	// InitiativeReview holds bonded reviewers' verdicts, keyed
	// (initiative_id, round, reviewer).
	InitiativeReview collections.Map[collections.Triple[uint64, uint32, string], types.InitiativeReview]
	// EscalatedReviews is the set of initiatives whose review round is with the
	// Operations Committee awaiting a decision.
	EscalatedReviews collections.KeySet[uint64]
	// RoleRewardDayFunding tracks the community-pool skim per UTC day.
	RoleRewardDayFunding collections.Map[uint64, string]
	// ReviewBounty is DREAM escrowed against an initiative to attract reviewers.
	ReviewBounty collections.Map[uint64, types.ReviewBounty]
	InterimSeq   collections.Sequence
	Interim      collections.Map[uint64, types.Interim]
	GiftRecord   collections.Map[collections.Pair[string, string], types.GiftRecord]

	// Secondary indexes for efficient lookups (avoid full table scans in EndBlocker)
	// Key: (status, id) - allows iteration by status
	InitiativesByStatus  collections.KeySet[collections.Pair[int32, uint64]]
	InterimsByStatus     collections.KeySet[collections.Pair[int32, uint64]]
	JuryReviewsByVerdict collections.KeySet[collections.Pair[int32, uint64]]
	JuryReviewsByJuror   collections.KeySet[collections.Pair[string, uint64]]
	// Key: (targetType, targetID, stakeID) - allows lookup of stakes by target
	StakesByTarget collections.KeySet[collections.Triple[int32, uint64, uint64]]
	// Key: (status, id) - allows iteration of challenges by status
	ChallengesByStatus collections.KeySet[collections.Pair[int32, uint64]]
	// Key: (status, id) - allows iteration of projects by status (currently
	// drives the EndBlocker sweep that expires stale PROPOSED projects).
	ProjectsByStatus collections.KeySet[collections.Pair[int32, uint64]]

	// Extended staking pools (for O(1) reward distribution)
	MemberStakePool  collections.Map[string, types.MemberStakePool]  // member address -> pool
	TagStakePool     collections.Map[string, types.TagStakePool]     // tag name -> pool
	ProjectStakeInfo collections.Map[uint64, types.ProjectStakeInfo] // project ID -> info

	// Content challenges
	ContentChallengeSeq       collections.Sequence
	ContentChallenge          collections.Map[uint64, types.ContentChallenge]
	ContentChallengesByStatus collections.KeySet[collections.Pair[int32, uint64]]
	// (targetType, targetID) -> challengeID — enforces one active challenge per content item
	ContentChallengesByTarget collections.Map[collections.Pair[int32, uint64], uint64]

	// Content-initiative links for conviction propagation
	// Key: (initiativeID, (targetType, targetID)) — enables prefix scan by initiative
	ContentInitiativeLinks collections.KeySet[collections.Pair[uint64, collections.Pair[int32, uint64]]]

	// Bounded conviction refresh. ConvictionQueue is a due-time-ordered work
	// list drained under a per-block stake budget in EndBlocker;
	// ConvictionScheduledAt is the reverse pointer that lets a reschedule remove
	// an initiative's existing entry. InitiativesByContent is the reverse of
	// ContentInitiativeLinks, so a content stake mutation can reschedule exactly
	// the initiatives whose propagated conviction it affected.
	ConvictionQueue       collections.KeySet[collections.Pair[int64, uint64]]
	ConvictionScheduledAt collections.Map[uint64, int64]
	InitiativesByContent  collections.KeySet[collections.Pair[collections.Pair[int32, uint64], uint64]]

	// Seasonal staking reward pool state (MasterChef-style accumulator)
	SeasonalPoolRemaining   collections.Item[string] // remaining DREAM in pool (as Int string)
	SeasonalPoolAccPerShare collections.Item[string] // accumulated reward per share (as Dec string)
	SeasonalPoolTotalStaked collections.Item[string] // total DREAM staked in initiatives + projects (as Int string)
	SeasonalPoolSeason      collections.Item[uint64] // which season this pool was initialized for

	// Treasury and economic tracking
	TreasuryBalance               collections.Item[string] // x/rep module treasury DREAM balance (as Int string)
	SeasonMinted                  collections.Item[string] // total DREAM minted this season (as Int string)
	SeasonBurned                  collections.Item[string] // total DREAM burned this season (as Int string)
	SeasonInitiativeRewardsMinted collections.Item[string] // DREAM minted via initiative completion this season (as Int string)
	SeasonTreasuryInflow          collections.Item[string] // DREAM credited to module treasury this season (as Int string)
	SeasonTreasuryOutflow         collections.Item[string] // DREAM spent from module treasury this season (as Int string)
	EpochMintedEpoch              collections.Item[uint64] // tracked epoch for the per-epoch mint counter
	EpochMintedAmount             collections.Item[string] // DREAM minted during tracked epoch (as Int string)
	DecayLastProcessedEpoch       collections.Item[uint64] // last epoch for which bulk DREAM decay has been applied

	// Tag registry (shared across content modules: forum, collect, rep/initiatives)
	Tag         collections.Map[string, types.Tag]
	ReservedTag collections.Map[string, types.ReservedTag]
	TagReport   collections.Map[string, types.TagReport]

	// Tag budgets (group-owned SPARK pools that reward tagged posts)
	TagBudget         collections.Map[uint64, types.TagBudget]
	TagBudgetSeq      collections.Sequence
	TagBudgetAward    collections.Map[uint64, types.TagBudgetAward]
	TagBudgetAwardSeq collections.Sequence
	// TagBudgetAwardByPost tracks the block height of the most recent award for each
	// (budget_id, post_id) pair to enforce a cooldown against single-post drain attacks.
	TagBudgetAwardByPost collections.Map[collections.Pair[uint64, uint64], int64]

	// Bonded-role primitive (generalization of sentinel / curator / verifier).
	// Key: (role_type int32, address string). Compound key lets one address
	// hold multiple roles; prefix scan on role_type yields list-by-type.
	BondedRoles       collections.Map[collections.Pair[int32, string], types.BondedRole]
	BondedRoleConfigs collections.Map[int32, types.BondedRoleConfig]
	RoleActivities    collections.Map[collections.Pair[int32, string], types.RoleActivity]

	// Accountability
	JuryParticipation  collections.Map[string, types.JuryParticipation]
	MemberReport       collections.Map[string, types.MemberReport]
	MemberWarning      collections.Map[uint64, types.MemberWarning]
	MemberWarningSeq   collections.Sequence
	GovActionAppeal    collections.Map[uint64, types.GovActionAppeal]
	GovActionAppealSeq collections.Sequence
}

func NewKeeper(
	storeService corestore.KVStoreService,
	cdc codec.Codec,
	addressCodec address.Codec,
	authority []byte,

	authKeeper types.AuthKeeper,
	bankKeeper types.BankKeeper,
	commonsKeeper types.CommonsKeeper,
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

		authKeeper:           authKeeper,
		bankKeeper:           bankKeeper,
		commonsKeeper:        commonsKeeper,
		late:                 &lateKeepers{},
		Params:               collections.NewItem(sb, types.ParamsKey, "params", codec.CollValue[types.Params](cdc)),
		Member:               collections.NewMap(sb, types.MemberKey, "member", collections.StringKey, codec.CollValue[types.Member](cdc)),
		Invitation:           collections.NewMap(sb, types.InvitationKey, "invitation", collections.Uint64Key, codec.CollValue[types.Invitation](cdc)),
		InvitationsByInvitee: collections.NewMap(sb, types.InvitationsByInviteeKey, "invitationsByInvitee", collections.StringKey, collections.Uint64Value),
		InvitationSeq:        collections.NewSequence(sb, types.InvitationCountKey, "invitationSequence"),
		Project:              collections.NewMap(sb, types.ProjectKey, "project", collections.Uint64Key, codec.CollValue[types.Project](cdc)),
		ProjectSeq:           collections.NewSequence(sb, types.ProjectCountKey, "projectSequence"),
		Initiative:           collections.NewMap(sb, types.InitiativeKey, "initiative", collections.Uint64Key, codec.CollValue[types.Initiative](cdc)),
		InitiativeSeq:        collections.NewSequence(sb, types.InitiativeCountKey, "initiativeSequence"),
		Stake:                collections.NewMap(sb, types.StakeKey, "stake", collections.Uint64Key, codec.CollValue[types.Stake](cdc)),
		StakeSeq:             collections.NewSequence(sb, types.StakeCountKey, "stakeSequence"),
		Challenge:            collections.NewMap(sb, types.ChallengeKey, "challenge", collections.Uint64Key, codec.CollValue[types.Challenge](cdc)),
		ChallengeSeq:         collections.NewSequence(sb, types.ChallengeCountKey, "challengeSequence"),
		JuryReview:           collections.NewMap(sb, types.JuryReviewKey, "juryReview", collections.Uint64Key, codec.CollValue[types.JuryReview](cdc)),
		EscalatedReviews:     collections.NewKeySet(sb, types.EscalatedReviewsKey, "escalatedReviews", collections.Uint64Key),
		RoleRewardDayFunding: collections.NewMap(sb, types.RoleRewardDayFundingKey, "roleRewardDayFunding",
			collections.Uint64Key, collections.StringValue),
		ReviewBounty: collections.NewMap(sb, types.ReviewBountyKey, "reviewBounty",
			collections.Uint64Key, codec.CollValue[types.ReviewBounty](cdc)),
		InitiativeReview: collections.NewMap(sb, types.InitiativeReviewKey, "initiativeReview",
			collections.TripleKeyCodec(collections.Uint64Key, collections.Uint32Key, collections.StringKey),
			codec.CollValue[types.InitiativeReview](cdc)),
		JuryReviewSeq: collections.NewSequence(sb, types.JuryReviewCountKey, "juryReviewSequence"),
		Interim:       collections.NewMap(sb, types.InterimKey, "interim", collections.Uint64Key, codec.CollValue[types.Interim](cdc)),
		InterimSeq:    collections.NewSequence(sb, types.InterimCountKey, "interimSequence"),
		GiftRecord: collections.NewMap(sb, types.GiftRecordKey, "giftRecord",
			collections.PairKeyCodec(collections.StringKey, collections.StringKey),
			codec.CollValue[types.GiftRecord](cdc)),

		// Secondary indexes for efficient EndBlocker operations
		InitiativesByStatus: collections.NewKeySet(
			sb, types.InitiativesByStatusKey, "initiativesByStatus",
			collections.PairKeyCodec(collections.Int32Key, collections.Uint64Key),
		),
		InterimsByStatus: collections.NewKeySet(
			sb, types.InterimsByStatusKey, "interimsByStatus",
			collections.PairKeyCodec(collections.Int32Key, collections.Uint64Key),
		),
		JuryReviewsByVerdict: collections.NewKeySet(
			sb, types.JuryReviewsByVerdictKey, "juryReviewsByVerdict",
			collections.PairKeyCodec(collections.Int32Key, collections.Uint64Key),
		),
		JuryReviewsByJuror: collections.NewKeySet(
			sb, types.JuryReviewsByJurorKey, "juryReviewsByJuror",
			collections.PairKeyCodec(collections.StringKey, collections.Uint64Key),
		),
		StakesByTarget: collections.NewKeySet(
			sb, types.StakesByTargetKey, "stakesByTarget",
			collections.TripleKeyCodec(collections.Int32Key, collections.Uint64Key, collections.Uint64Key),
		),
		ChallengesByStatus: collections.NewKeySet(
			sb, types.ChallengesByStatusKey, "challengesByStatus",
			collections.PairKeyCodec(collections.Int32Key, collections.Uint64Key),
		),
		ProjectsByStatus: collections.NewKeySet(
			sb, types.ProjectsByStatusKey, "projectsByStatus",
			collections.PairKeyCodec(collections.Int32Key, collections.Uint64Key),
		),

		// Extended staking pools
		MemberStakePool:  collections.NewMap(sb, types.MemberStakePoolKey, "memberStakePool", collections.StringKey, codec.CollValue[types.MemberStakePool](cdc)),
		TagStakePool:     collections.NewMap(sb, types.TagStakePoolKey, "tagStakePool", collections.StringKey, codec.CollValue[types.TagStakePool](cdc)),
		ProjectStakeInfo: collections.NewMap(sb, types.ProjectStakeInfoKey, "projectStakeInfo", collections.Uint64Key, codec.CollValue[types.ProjectStakeInfo](cdc)),

		// Content challenges
		ContentChallenge:    collections.NewMap(sb, types.ContentChallengeKey, "contentChallenge", collections.Uint64Key, codec.CollValue[types.ContentChallenge](cdc)),
		ContentChallengeSeq: collections.NewSequence(sb, types.ContentChallengeCountKey, "contentChallengeSequence"),
		ContentChallengesByStatus: collections.NewKeySet(
			sb, types.ContentChallengesByStatusKey, "contentChallengesByStatus",
			collections.PairKeyCodec(collections.Int32Key, collections.Uint64Key),
		),
		ContentChallengesByTarget: collections.NewMap(
			sb, types.ContentChallengesByTargetKey, "contentChallengesByTarget",
			collections.PairKeyCodec(collections.Int32Key, collections.Uint64Key),
			collections.Uint64Value,
		),

		// Content-initiative links for conviction propagation
		ContentInitiativeLinks: collections.NewKeySet(
			sb, types.ContentInitiativeLinksKey, "contentInitiativeLinks",
			collections.PairKeyCodec(collections.Uint64Key, collections.PairKeyCodec(collections.Int32Key, collections.Uint64Key)),
		),
		ConvictionQueue: collections.NewKeySet(
			sb, types.ConvictionQueueKey, "convictionQueue",
			collections.PairKeyCodec(collections.Int64Key, collections.Uint64Key),
		),
		ConvictionScheduledAt: collections.NewMap(
			sb, types.ConvictionScheduledAtKey, "convictionScheduledAt",
			collections.Uint64Key, collections.Int64Value,
		),
		InitiativesByContent: collections.NewKeySet(
			sb, types.InitiativesByContentKey, "initiativesByContent",
			collections.PairKeyCodec(collections.PairKeyCodec(collections.Int32Key, collections.Uint64Key), collections.Uint64Key),
		),

		// Seasonal staking reward pool state
		SeasonalPoolRemaining:   collections.NewItem(sb, types.SeasonalPoolRemainingKey, "seasonalPoolRemaining", collections.StringValue),
		SeasonalPoolAccPerShare: collections.NewItem(sb, types.SeasonalPoolAccPerShareKey, "seasonalPoolAccPerShare", collections.StringValue),
		SeasonalPoolTotalStaked: collections.NewItem(sb, types.SeasonalPoolTotalStakedKey, "seasonalPoolTotalStaked", collections.StringValue),
		SeasonalPoolSeason:      collections.NewItem(sb, types.SeasonalPoolSeasonKey, "seasonalPoolSeason", collections.Uint64Value),

		// Treasury and economic tracking
		TreasuryBalance:               collections.NewItem(sb, types.TreasuryBalanceKey, "treasuryBalance", collections.StringValue),
		SeasonMinted:                  collections.NewItem(sb, types.SeasonMintedKey, "seasonMinted", collections.StringValue),
		SeasonBurned:                  collections.NewItem(sb, types.SeasonBurnedKey, "seasonBurned", collections.StringValue),
		SeasonInitiativeRewardsMinted: collections.NewItem(sb, types.SeasonInitiativeRewardsMintedKey, "seasonInitiativeRewards", collections.StringValue),
		SeasonTreasuryInflow:          collections.NewItem(sb, types.SeasonTreasuryInflowKey, "seasonTreasuryInflow", collections.StringValue),
		SeasonTreasuryOutflow:         collections.NewItem(sb, types.SeasonTreasuryOutflowKey, "seasonTreasuryOutflow", collections.StringValue),
		EpochMintedEpoch:              collections.NewItem(sb, types.EpochMintedEpochKey, "epochMintedEpoch", collections.Uint64Value),
		EpochMintedAmount:             collections.NewItem(sb, types.EpochMintedAmountKey, "epochMintedAmount", collections.StringValue),
		DecayLastProcessedEpoch:       collections.NewItem(sb, types.DecayLastProcessedEpochKey, "decayLastProcessedEpoch", collections.Uint64Value),

		// Tag registry
		Tag:         collections.NewMap(sb, types.TagKey, "tag", collections.StringKey, codec.CollValue[types.Tag](cdc)),
		ReservedTag: collections.NewMap(sb, types.ReservedTagKey, "reservedTag", collections.StringKey, codec.CollValue[types.ReservedTag](cdc)),
		TagReport:   collections.NewMap(sb, types.TagReportKey, "tagReport", collections.StringKey, codec.CollValue[types.TagReport](cdc)),

		// Tag budgets
		TagBudget:         collections.NewMap(sb, types.TagBudgetKey, "tagBudget", collections.Uint64Key, codec.CollValue[types.TagBudget](cdc)),
		TagBudgetSeq:      collections.NewSequence(sb, types.TagBudgetCountKey, "tagBudgetSequence"),
		TagBudgetAward:    collections.NewMap(sb, types.TagBudgetAwardKey, "tagBudgetAward", collections.Uint64Key, codec.CollValue[types.TagBudgetAward](cdc)),
		TagBudgetAwardSeq: collections.NewSequence(sb, types.TagBudgetAwardCountKey, "tagBudgetAwardSequence"),
		TagBudgetAwardByPost: collections.NewMap(
			sb, types.TagBudgetAwardByPostKey, "tagBudgetAwardByPost",
			collections.PairKeyCodec(collections.Uint64Key, collections.Uint64Key),
			collections.Int64Value,
		),

		// Bonded-role primitive
		BondedRoles: collections.NewMap(
			sb, types.BondedRoleKey, "bondedRoles",
			collections.PairKeyCodec(collections.Int32Key, collections.StringKey),
			codec.CollValue[types.BondedRole](cdc),
		),
		BondedRoleConfigs: collections.NewMap(
			sb, types.BondedRoleConfigKey, "bondedRoleConfigs",
			collections.Int32Key,
			codec.CollValue[types.BondedRoleConfig](cdc),
		),
		RoleActivities: collections.NewMap(
			sb, types.RoleActivityKey, "roleActivities",
			collections.PairKeyCodec(collections.Int32Key, collections.StringKey),
			codec.CollValue[types.RoleActivity](cdc),
		),

		// Accountability
		JuryParticipation:  collections.NewMap(sb, types.JuryParticipationKey, "juryParticipation", collections.StringKey, codec.CollValue[types.JuryParticipation](cdc)),
		MemberReport:       collections.NewMap(sb, types.MemberReportKey, "memberReport", collections.StringKey, codec.CollValue[types.MemberReport](cdc)),
		MemberWarning:      collections.NewMap(sb, types.MemberWarningKey, "memberWarning", collections.Uint64Key, codec.CollValue[types.MemberWarning](cdc)),
		MemberWarningSeq:   collections.NewSequence(sb, types.MemberWarningCountKey, "memberWarningSequence"),
		GovActionAppeal:    collections.NewMap(sb, types.GovActionAppealKey, "govActionAppeal", collections.Uint64Key, codec.CollValue[types.GovActionAppeal](cdc)),
		GovActionAppealSeq: collections.NewSequence(sb, types.GovActionAppealCountKey, "govActionAppealSequence"),
	}
	schema, err := sb.Build()
	if err != nil {
		panic(err)
	}
	k.Schema = schema

	return k
}

// SetSeasonKeeper sets the season keeper after depinject initialization.
// This breaks the cyclic dependency: rep → season → collect/blog/forum → rep.
// Uses the shared lateKeepers so all value copies see the update.
func (k Keeper) SetSeasonKeeper(sk types.SeasonKeeper) {
	k.late.seasonKeeper = sk
}

// SetDistrKeeper sets the distribution keeper after depinject initialization.
// Lets the bonded-role reward pools top themselves up from the community pool
// instead of waiting on a council to remember to fund them.
func (k Keeper) SetDistrKeeper(dk types.DistrKeeper) {
	k.late.distrKeeper = dk
}

// SetMintKeeper sets the mint keeper after depinject initialization. Supplies
// annual provisions, which size the bonded-role funding draw.
func (k Keeper) SetMintKeeper(mk types.MintKeeper) {
	k.late.mintKeeper = mk
}

// SetForumKeeper sets the forum keeper after depinject initialization.
// Late-wired so rep can ask forum to prune stale tag references when a tag is
// removed by moderation. Will be retired when forum's sentinel state moves
// into x/rep.
func (k Keeper) SetForumKeeper(fk types.ForumKeeper) {
	k.late.forumKeeper = fk
}

// SetBlogKeeper sets the blog keeper after depinject initialization.
// Used by stake validation to resolve true content authors for self-stake
// prevention.
func (k Keeper) SetBlogKeeper(bk types.BlogKeeper) {
	k.late.blogKeeper = bk
}

// SetCollectKeeper sets the collect keeper after depinject initialization.
// Used by stake validation to resolve true collection owners for self-stake
// prevention.
func (k Keeper) SetCollectKeeper(ck types.CollectKeeper) {
	k.late.collectKeeper = ck
}

// SetIdentityKeeper late-binds the identity keeper used by BondDenom /
// DreamDenom helpers to resolve the chain's federated denoms.
func (k Keeper) SetIdentityKeeper(idk types.IdentityKeeper) {
	k.late.identityKeeper = idk
}

// SetHooks late-binds the RepHooks dispatcher. Idempotent overwrite; wrap
// multiple subscribers in types.NewMultiRepHooks(...) before calling. Hook
// invocations are non-tx-halting so a buggy downstream module cannot brick
// member admission.
func (k Keeper) SetHooks(h types.RepHooks) {
	k.late.hooks = h
}

// Hooks returns the wired RepHooks dispatcher, or a no-op fallback if none
// has been wired yet. Callers can invoke hook methods without nil-checking.
func (k Keeper) Hooks() types.RepHooks {
	if k.late.hooks == nil {
		return types.MultiRepHooks{}
	}
	return k.late.hooks
}

// BondDenom returns the chain's bond denom from the wired identity keeper.
// Panics if identity isn't wired: no silent fallback to a hardcoded literal.
func (k Keeper) BondDenom(ctx context.Context) string {
	if k.late.identityKeeper == nil {
		panic("rep keeper: identityKeeper not wired (call SetIdentityKeeper after depinject)")
	}
	return k.late.identityKeeper.BondDenom(ctx)
}

// DreamDenom returns the chain's DREAM denom from the wired identity keeper.
// Panics if identity isn't wired: no silent fallback to a hardcoded literal.
func (k Keeper) DreamDenom(ctx context.Context) string {
	if k.late.identityKeeper == nil {
		panic("rep keeper: identityKeeper not wired (call SetIdentityKeeper after depinject)")
	}
	return k.late.identityKeeper.DreamDenom(ctx)
}

// GetAuthority returns the module's authority.
func (k Keeper) GetAuthority() []byte {
	return k.authority
}

// GetAuthorityString returns the module's authority as a string.
func (k Keeper) GetAuthorityString() string {
	addr, _ := k.addressCodec.BytesToString(k.authority)
	return addr
}

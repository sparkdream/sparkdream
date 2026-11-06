package keeper

import (
	"fmt"

	"cosmossdk.io/collections"
	"cosmossdk.io/core/address"
	corestore "cosmossdk.io/core/store"
	"github.com/cosmos/cosmos-sdk/codec"

	"sparkdream/internal/dreamutil"
	"sparkdream/x/season/types"
)

type Keeper struct {
	storeService corestore.KVStoreService
	cdc          codec.Codec
	addressCodec address.Codec
	// Address capable of executing a MsgUpdateParams message.
	// Typically, this should be the x/gov module account.
	authority []byte

	Schema collections.Schema
	Params collections.Item[types.Params]

	// External module keepers for cross-module integration
	bankKeeper types.BankKeeper

	// Shared DREAM token operations (delegates to repKeeper)
	Season                  collections.Item[types.Season]
	SeasonHistory           collections.Map[uint64, types.Season] // completed seasons by number
	SeasonTransitionState   collections.Item[types.SeasonTransitionState]
	TransitionRecoveryState collections.Item[types.TransitionRecoveryState]
	NextSeasonInfo          collections.Item[types.NextSeasonInfo]
	SeasonSnapshot          collections.Map[uint64, types.SeasonSnapshot]
	MemberSeasonSnapshot    collections.Map[string, types.MemberSeasonSnapshot]
	MemberProfile           collections.Map[string, types.MemberProfile]
	MemberRegistration      collections.Map[string, types.MemberRegistration]
	Achievement             collections.Map[string, types.Achievement]
	Title                   collections.Map[string, types.Title]
	// Secondary indexes (requirement type, id), rebuilt at InitGenesis and
	// kept in step by SetAchievement/RemoveAchievement/SetTitle/RemoveTitle.
	AchievementsByRequirement collections.KeySet[collections.Pair[int32, string]]
	TitlesByRequirement       collections.KeySet[collections.Pair[int32, string]]
	SeasonTitleEligibility    collections.Map[uint64, types.SeasonTitleEligibility]
	GuildSeq                  collections.Sequence
	Guild                     *collections.IndexedMap[uint64, types.Guild, GuildIndexes]
	GuildMembership           *collections.IndexedMap[string, types.GuildMembership, GuildMembershipIndexes]
	GuildInvite               *collections.IndexedMap[string, types.GuildInvite, GuildInviteIndexes]
	GuildMemberCount          collections.Map[uint64, uint64] // guildID -> active member count
	Quest                     collections.Map[string, types.Quest]
	MemberQuestProgress       *collections.IndexedMap[string, types.MemberQuestProgress, QuestProgressIndexes]
	EpochXpTracker            collections.Map[string, types.EpochXpTracker]
	VoteXpRecord              collections.Map[string, types.VoteXpRecord]
	ForumXpCooldown           collections.Map[string, types.ForumXpCooldown]
	DisplayNameModeration     collections.Map[string, types.DisplayNameModeration]
	DisplayNameReportStake    *collections.IndexedMap[string, types.DisplayNameReportStake, DisplayNameReportStakeIndexes]
	DisplayNameAppealStake    *collections.IndexedMap[string, types.DisplayNameAppealStake, DisplayNameAppealStakeIndexes]
	NominationSeq             collections.Sequence
	Nomination                collections.Map[uint64, types.Nomination]
	NominationStake           *collections.IndexedMap[string, types.NominationStake, NominationStakeIndexes] // key: "nominationId/staker"
	RetroRewardRecord         collections.Map[string, types.RetroRewardRecord]                               // key: "season/nominationId"
	TransitionAborts          collections.Map[uint64, uint64]                                                // season -> aborts so far
	PruneCursor               collections.Map[string, string]                                                // collection -> last pruned key (SEA-11)
	NominationsBySeason       collections.KeySet[collections.Pair[uint64, uint64]]                           // (season, nominationID)
	ModerationQueue           collections.KeySet[collections.Pair[int64, string]]                            // (moderated_at, member), active + unappealed only
	AppealQueue               collections.KeySet[collections.Pair[int64, string]]                            // (appealed_at, member), active + appealed only (SEA-23)
	RetroPayoutRetry          collections.KeySet[collections.Pair[uint64, uint64]]                           // (season, nominationID), failed payouts (SEA-21)
	PendingSeasonalPoolInit   collections.Item[uint64]                                                       // season whose rep pool init is owed (SEA-22)
	UsernameOwner             collections.Map[string, string]                                                // lowercase username -> member (SEA-28)
	NominationsByRef          collections.KeySet[collections.Pair[uint64, string]]                           // (season, content_ref) (SEA-20)
	NominationCountBy         collections.Map[collections.Pair[uint64, string], uint64]                      // (season, nominator) -> count (SEA-20)
	ModerationRetryQueue      collections.KeySet[collections.Pair[int64, string]]                            // (auto_resolve_retry_at, member), failed auto-resolutions (SEA-09)
	SeasonStatsCounters       collections.Map[uint64, types.SeasonStatsCounters]                             // past season -> final aggregates (SEA-35)
	CurrentSeasonStats        collections.Item[types.SeasonStatsCounters]                                    // current season's running aggregates (SEA-35)
	PendingVoteQuestProgress  collections.KeySet[collections.Pair[string, uint64]]                           // (member, proposal) votes not yet applied to quest objectives (SEA-103)

	// Optional content-module keepers for retroactive public goods funding
	// late holds the keepers wired after depinject (app.go Set*Keeper).
	// It is a pointer so the AppModule's value copy of the keeper sees
	// them too (docs/development-conventions.md, AppModule value-copy bug).
	late *lateKeepers
}

// lateKeepers are the dependencies wired after construction; shared by
// every copy of the Keeper value.
type lateKeepers struct {
	repKeeper      types.RepKeeper
	nameKeeper     types.NameKeeper
	commonsKeeper  types.CommonsKeeper
	blogKeeper     types.BlogKeeper
	forumKeeper    types.ForumKeeper
	collectKeeper  types.CollectKeeper
	artifactKeeper types.ArtifactKeeper
	dreamOps       dreamutil.Ops
}

func NewKeeper(
	storeService corestore.KVStoreService,
	cdc codec.Codec,
	addressCodec address.Codec,
	authority []byte,

	bankKeeper types.BankKeeper,
) Keeper {
	if _, err := addressCodec.BytesToString(authority); err != nil {
		panic(fmt.Sprintf("invalid authority address %s: %s", authority, err))
	}

	sb := collections.NewSchemaBuilder(storeService)

	k := Keeper{
		late:         &lateKeepers{},
		storeService: storeService,
		cdc:          cdc,
		addressCodec: addressCodec,
		authority:    authority,

		bankKeeper: bankKeeper,
		Params:     collections.NewItem(sb, types.ParamsKey, "params", codec.CollValue[types.Params](cdc)),
		Season:     collections.NewItem(sb, types.SeasonKey, "season", codec.CollValue[types.Season](cdc)), SeasonHistory: collections.NewMap(sb, types.SeasonHistoryKey, "seasonHistory", collections.Uint64Key, codec.CollValue[types.Season](cdc)), SeasonTransitionState: collections.NewItem(sb, types.SeasonTransitionStateKey, "seasonTransitionState", codec.CollValue[types.SeasonTransitionState](cdc)), TransitionRecoveryState: collections.NewItem(sb, types.TransitionRecoveryStateKey, "transitionRecoveryState", codec.CollValue[types.TransitionRecoveryState](cdc)), NextSeasonInfo: collections.NewItem(sb, types.NextSeasonInfoKey, "nextSeasonInfo", codec.CollValue[types.NextSeasonInfo](cdc)), SeasonSnapshot: collections.NewMap(sb, types.SeasonSnapshotKey, "seasonSnapshot", collections.Uint64Key, codec.CollValue[types.SeasonSnapshot](cdc)), MemberSeasonSnapshot: collections.NewMap(sb, types.MemberSeasonSnapshotKey, "memberSeasonSnapshot", collections.StringKey, codec.CollValue[types.MemberSeasonSnapshot](cdc)), MemberProfile: collections.NewMap(sb, types.MemberProfileKey, "memberProfile", collections.StringKey, codec.CollValue[types.MemberProfile](cdc)), MemberRegistration: collections.NewMap(sb, types.MemberRegistrationKey, "memberRegistration", collections.StringKey, codec.CollValue[types.MemberRegistration](cdc)), Achievement: collections.NewMap(sb, types.AchievementKey, "achievement", collections.StringKey, codec.CollValue[types.Achievement](cdc)), Title: collections.NewMap(sb, types.TitleKey, "title", collections.StringKey, codec.CollValue[types.Title](cdc)),
		AchievementsByRequirement: collections.NewKeySet(sb, types.AchievementsByRequirementKey, "achievementsByRequirement", collections.PairKeyCodec(collections.Int32Key, collections.StringKey)),
		TitlesByRequirement:       collections.NewKeySet(sb, types.TitlesByRequirementKey, "titlesByRequirement", collections.PairKeyCodec(collections.Int32Key, collections.StringKey)), SeasonTitleEligibility: collections.NewMap(sb, types.SeasonTitleEligibilityKey, "seasonTitleEligibility", collections.Uint64Key, codec.CollValue[types.SeasonTitleEligibility](cdc)), Guild: collections.NewIndexedMap(sb, types.GuildKey, "guild", collections.Uint64Key, codec.CollValue[types.Guild](cdc), newGuildIndexes(sb)),
		GuildSeq:         collections.NewSequence(sb, types.GuildCountKey, "guildSequence"),
		GuildMemberCount: collections.NewMap(sb, types.GuildMemberCountKey, "guildMemberCount", collections.Uint64Key, collections.Uint64Value),
		GuildMembership:  collections.NewIndexedMap(sb, types.GuildMembershipKey, "guildMembership", collections.StringKey, codec.CollValue[types.GuildMembership](cdc), newGuildMembershipIndexes(sb)), GuildInvite: collections.NewIndexedMap(sb, types.GuildInviteKey, "guildInvite", collections.StringKey, codec.CollValue[types.GuildInvite](cdc), newGuildInviteIndexes(sb)), Quest: collections.NewMap(sb, types.QuestKey, "quest", collections.StringKey, codec.CollValue[types.Quest](cdc)), MemberQuestProgress: collections.NewIndexedMap(sb, types.MemberQuestProgressKey, "memberQuestProgress", collections.StringKey, codec.CollValue[types.MemberQuestProgress](cdc), newQuestProgressIndexes(sb)), EpochXpTracker: collections.NewMap(sb, types.EpochXpTrackerKey, "epochXpTracker", collections.StringKey, codec.CollValue[types.EpochXpTracker](cdc)), VoteXpRecord: collections.NewMap(sb, types.VoteXpRecordKey, "voteXpRecord", collections.StringKey, codec.CollValue[types.VoteXpRecord](cdc)), ForumXpCooldown: collections.NewMap(sb, types.ForumXpCooldownKey, "forumXpCooldown", collections.StringKey, codec.CollValue[types.ForumXpCooldown](cdc)), DisplayNameModeration: collections.NewMap(sb, types.DisplayNameModerationKey, "displayNameModeration", collections.StringKey, codec.CollValue[types.DisplayNameModeration](cdc)), DisplayNameReportStake: collections.NewIndexedMap(sb, types.DisplayNameReportStakeKey, "displayNameReportStake", collections.StringKey, codec.CollValue[types.DisplayNameReportStake](cdc), newDisplayNameReportStakeIndexes(sb)), DisplayNameAppealStake: collections.NewIndexedMap(sb, types.DisplayNameAppealStakeKey, "displayNameAppealStake", collections.StringKey, codec.CollValue[types.DisplayNameAppealStake](cdc), newDisplayNameAppealStakeIndexes(sb)),
		NominationSeq:     collections.NewSequence(sb, types.NominationCountKey, "nominationSequence"),
		Nomination:        collections.NewMap(sb, types.NominationKey, "nomination", collections.Uint64Key, codec.CollValue[types.Nomination](cdc)),
		NominationStake:   collections.NewIndexedMap(sb, types.NominationStakeKey, "nominationStake", collections.StringKey, codec.CollValue[types.NominationStake](cdc), newNominationStakeIndexes(sb)),
		RetroRewardRecord: collections.NewMap(sb, types.RetroRewardRecordKey, "retroRewardRecord", collections.StringKey, codec.CollValue[types.RetroRewardRecord](cdc)),
		TransitionAborts:  collections.NewMap(sb, types.TransitionAbortsKey, "transitionAborts", collections.Uint64Key, collections.Uint64Value),
		PruneCursor:       collections.NewMap(sb, types.PruneCursorKey, "pruneCursor", collections.StringKey, collections.StringValue),
		NominationsBySeason: collections.NewKeySet(sb, types.NominationsBySeasonKey, "nominationsBySeason",
			collections.PairKeyCodec(collections.Uint64Key, collections.Uint64Key)),
		ModerationQueue: collections.NewKeySet(sb, types.ModerationQueueKey, "moderationQueue",
			collections.PairKeyCodec(collections.Int64Key, collections.StringKey)),
		AppealQueue: collections.NewKeySet(sb, types.AppealQueueKey, "appealQueue",
			collections.PairKeyCodec(collections.Int64Key, collections.StringKey)),
		RetroPayoutRetry: collections.NewKeySet(sb, types.RetroPayoutRetryKey, "retroPayoutRetry",
			collections.PairKeyCodec(collections.Uint64Key, collections.Uint64Key)),
		PendingSeasonalPoolInit: collections.NewItem(sb, types.PendingSeasonalPoolInitKey, "pendingSeasonalPoolInit", collections.Uint64Value),
		UsernameOwner:           collections.NewMap(sb, types.UsernameOwnerKey, "usernameOwner", collections.StringKey, collections.StringValue),
		NominationsByRef: collections.NewKeySet(sb, types.NominationsByRefKey, "nominationsByRef",
			collections.PairKeyCodec(collections.Uint64Key, collections.StringKey)),
		NominationCountBy: collections.NewMap(sb, types.NominationCountByNominatorKey, "nominationCountBy",
			collections.PairKeyCodec(collections.Uint64Key, collections.StringKey), collections.Uint64Value),
		ModerationRetryQueue: collections.NewKeySet(sb, types.ModerationRetryQueueKey, "moderationRetryQueue",
			collections.PairKeyCodec(collections.Int64Key, collections.StringKey)),
		SeasonStatsCounters: collections.NewMap(sb, types.SeasonStatsCountersKey, "seasonStatsCounters", collections.Uint64Key, codec.CollValue[types.SeasonStatsCounters](cdc)),
		CurrentSeasonStats:  collections.NewItem(sb, types.CurrentSeasonStatsKey, "currentSeasonStats", codec.CollValue[types.SeasonStatsCounters](cdc)),
		PendingVoteQuestProgress: collections.NewKeySet(sb, types.PendingVoteQuestProgressKey, "pendingVoteQuestProgress",
			collections.PairKeyCodec(collections.StringKey, collections.Uint64Key)),
	}
	schema, err := sb.Build()
	if err != nil {
		panic(err)
	}
	k.Schema = schema
	// dreamOps initialized lazily in SetRepKeeper (repKeeper is wired post-depinject)

	return k
}

// SetRepKeeper sets the RepKeeper after depinject to break cyclic dependencies.
func (k *Keeper) SetRepKeeper(rk types.RepKeeper) {
	k.late.repKeeper = rk
	k.late.dreamOps = dreamutil.NewOps(rk, k.addressCodec)
}

// SetNameKeeper sets the NameKeeper after depinject.
func (k *Keeper) SetNameKeeper(nk types.NameKeeper) {
	k.late.nameKeeper = nk
}

// SetCommonsKeeper sets the CommonsKeeper after depinject.
func (k *Keeper) SetCommonsKeeper(ck types.CommonsKeeper) {
	k.late.commonsKeeper = ck
}

// SetBlogKeeper sets the BlogKeeper after depinject.
func (k *Keeper) SetBlogKeeper(bk types.BlogKeeper) {
	k.late.blogKeeper = bk
}

// SetForumKeeper sets the ForumKeeper after depinject.
func (k *Keeper) SetForumKeeper(fk types.ForumKeeper) {
	k.late.forumKeeper = fk
}

// SetCollectKeeper sets the CollectKeeper after depinject.
func (k *Keeper) SetCollectKeeper(ck types.CollectKeeper) {
	k.late.collectKeeper = ck
}

// SetArtifactKeeper sets the ArtifactKeeper after depinject.
func (k *Keeper) SetArtifactKeeper(ak types.ArtifactKeeper) {
	k.late.artifactKeeper = ak
}

// GetAuthority returns the module's authority.
func (k Keeper) GetAuthority() []byte {
	return k.authority
}

// GetAddressCodec returns the module's address codec.
func (k Keeper) GetAddressCodec() address.Codec {
	return k.addressCodec
}

// GetBlogKeeper returns the optional blog keeper.
func (k Keeper) GetBlogKeeper() types.BlogKeeper { return k.late.blogKeeper }

// GetForumKeeper returns the optional forum keeper.
func (k Keeper) GetForumKeeper() types.ForumKeeper { return k.late.forumKeeper }

// GetCollectKeeper returns the optional collect keeper.
func (k Keeper) GetCollectKeeper() types.CollectKeeper { return k.late.collectKeeper }

// GetArtifactKeeper returns the optional artifact keeper.
func (k Keeper) GetArtifactKeeper() types.ArtifactKeeper { return k.late.artifactKeeper }

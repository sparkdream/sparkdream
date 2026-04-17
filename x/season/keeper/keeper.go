package keeper

import (
	"fmt"

	"cosmossdk.io/collections"
	"cosmossdk.io/core/address"
	corestore "cosmossdk.io/core/store"
	"github.com/cosmos/cosmos-sdk/codec"

	"sparkdream/lib/dreamutil"
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
	bankKeeper    types.BankKeeper
	repKeeper     types.RepKeeper
	nameKeeper    types.NameKeeper
	commonsKeeper types.CommonsKeeper

	// Shared DREAM token operations (delegates to repKeeper)
	dreamOps                dreamutil.Ops
	Season                  collections.Item[types.Season]
	SeasonTransitionState   collections.Item[types.SeasonTransitionState]
	TransitionRecoveryState collections.Item[types.TransitionRecoveryState]
	NextSeasonInfo          collections.Item[types.NextSeasonInfo]
	SeasonSnapshot          collections.Map[uint64, types.SeasonSnapshot]
	MemberSeasonSnapshot    collections.Map[string, types.MemberSeasonSnapshot]
	MemberProfile           collections.Map[string, types.MemberProfile]
	MemberRegistration      collections.Map[string, types.MemberRegistration]
	Achievement             collections.Map[string, types.Achievement]
	Title                   collections.Map[string, types.Title]
	SeasonTitleEligibility  collections.Map[uint64, types.SeasonTitleEligibility]
	GuildSeq                collections.Sequence
	Guild                   collections.Map[uint64, types.Guild]
	GuildMembership         collections.Map[string, types.GuildMembership]
	GuildInvite             collections.Map[string, types.GuildInvite]
	GuildMemberCount        collections.Map[uint64, uint64] // guildID -> active member count
	Quest                   collections.Map[string, types.Quest]
	MemberQuestProgress     collections.Map[string, types.MemberQuestProgress]
	EpochXpTracker          collections.Map[string, types.EpochXpTracker]
	VoteXpRecord            collections.Map[string, types.VoteXpRecord]
	ForumXpCooldown         collections.Map[string, types.ForumXpCooldown]
	DisplayNameModeration   collections.Map[string, types.DisplayNameModeration]
	DisplayNameReportStake  collections.Map[string, types.DisplayNameReportStake]
	DisplayNameAppealStake  collections.Map[string, types.DisplayNameAppealStake]
	NominationSeq           collections.Sequence
	Nomination              collections.Map[uint64, types.Nomination]
	NominationStake         collections.Map[string, types.NominationStake]   // key: "nominationId/staker"
	RetroRewardRecord       collections.Map[string, types.RetroRewardRecord] // key: "season/nominationId"

	// Optional content-module keepers for retroactive public goods funding
	blogKeeper    types.BlogKeeper
	forumKeeper   types.ForumKeeper
	collectKeeper types.CollectKeeper
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
		storeService: storeService,
		cdc:          cdc,
		addressCodec: addressCodec,
		authority:    authority,

		bankKeeper: bankKeeper,
		Params:     collections.NewItem(sb, types.ParamsKey, "params", codec.CollValue[types.Params](cdc)),
		Season:     collections.NewItem(sb, types.SeasonKey, "season", codec.CollValue[types.Season](cdc)), SeasonTransitionState: collections.NewItem(sb, types.SeasonTransitionStateKey, "seasonTransitionState", codec.CollValue[types.SeasonTransitionState](cdc)), TransitionRecoveryState: collections.NewItem(sb, types.TransitionRecoveryStateKey, "transitionRecoveryState", codec.CollValue[types.TransitionRecoveryState](cdc)), NextSeasonInfo: collections.NewItem(sb, types.NextSeasonInfoKey, "nextSeasonInfo", codec.CollValue[types.NextSeasonInfo](cdc)), SeasonSnapshot: collections.NewMap(sb, types.SeasonSnapshotKey, "seasonSnapshot", collections.Uint64Key, codec.CollValue[types.SeasonSnapshot](cdc)), MemberSeasonSnapshot: collections.NewMap(sb, types.MemberSeasonSnapshotKey, "memberSeasonSnapshot", collections.StringKey, codec.CollValue[types.MemberSeasonSnapshot](cdc)), MemberProfile: collections.NewMap(sb, types.MemberProfileKey, "memberProfile", collections.StringKey, codec.CollValue[types.MemberProfile](cdc)), MemberRegistration: collections.NewMap(sb, types.MemberRegistrationKey, "memberRegistration", collections.StringKey, codec.CollValue[types.MemberRegistration](cdc)), Achievement: collections.NewMap(sb, types.AchievementKey, "achievement", collections.StringKey, codec.CollValue[types.Achievement](cdc)), Title: collections.NewMap(sb, types.TitleKey, "title", collections.StringKey, codec.CollValue[types.Title](cdc)), SeasonTitleEligibility: collections.NewMap(sb, types.SeasonTitleEligibilityKey, "seasonTitleEligibility", collections.Uint64Key, codec.CollValue[types.SeasonTitleEligibility](cdc)), Guild: collections.NewMap(sb, types.GuildKey, "guild", collections.Uint64Key, codec.CollValue[types.Guild](cdc)),
		GuildSeq:         collections.NewSequence(sb, types.GuildCountKey, "guildSequence"),
		GuildMemberCount: collections.NewMap(sb, types.GuildMemberCountKey, "guildMemberCount", collections.Uint64Key, collections.Uint64Value),
		GuildMembership: collections.NewMap(sb, types.GuildMembershipKey, "guildMembership", collections.StringKey, codec.CollValue[types.GuildMembership](cdc)), GuildInvite: collections.NewMap(sb, types.GuildInviteKey, "guildInvite", collections.StringKey, codec.CollValue[types.GuildInvite](cdc)), Quest: collections.NewMap(sb, types.QuestKey, "quest", collections.StringKey, codec.CollValue[types.Quest](cdc)), MemberQuestProgress: collections.NewMap(sb, types.MemberQuestProgressKey, "memberQuestProgress", collections.StringKey, codec.CollValue[types.MemberQuestProgress](cdc)), EpochXpTracker: collections.NewMap(sb, types.EpochXpTrackerKey, "epochXpTracker", collections.StringKey, codec.CollValue[types.EpochXpTracker](cdc)), VoteXpRecord: collections.NewMap(sb, types.VoteXpRecordKey, "voteXpRecord", collections.StringKey, codec.CollValue[types.VoteXpRecord](cdc)), ForumXpCooldown: collections.NewMap(sb, types.ForumXpCooldownKey, "forumXpCooldown", collections.StringKey, codec.CollValue[types.ForumXpCooldown](cdc)), DisplayNameModeration: collections.NewMap(sb, types.DisplayNameModerationKey, "displayNameModeration", collections.StringKey, codec.CollValue[types.DisplayNameModeration](cdc)), DisplayNameReportStake: collections.NewMap(sb, types.DisplayNameReportStakeKey, "displayNameReportStake", collections.StringKey, codec.CollValue[types.DisplayNameReportStake](cdc)), DisplayNameAppealStake: collections.NewMap(sb, types.DisplayNameAppealStakeKey, "displayNameAppealStake", collections.StringKey, codec.CollValue[types.DisplayNameAppealStake](cdc)),
		NominationSeq:     collections.NewSequence(sb, types.NominationCountKey, "nominationSequence"),
		Nomination:        collections.NewMap(sb, types.NominationKey, "nomination", collections.Uint64Key, codec.CollValue[types.Nomination](cdc)),
		NominationStake:   collections.NewMap(sb, types.NominationStakeKey, "nominationStake", collections.StringKey, codec.CollValue[types.NominationStake](cdc)),
		RetroRewardRecord: collections.NewMap(sb, types.RetroRewardRecordKey, "retroRewardRecord", collections.StringKey, codec.CollValue[types.RetroRewardRecord](cdc)),
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
	k.repKeeper = rk
	k.dreamOps = dreamutil.NewOps(rk, k.addressCodec)
}

// SetNameKeeper sets the NameKeeper after depinject.
func (k *Keeper) SetNameKeeper(nk types.NameKeeper) {
	k.nameKeeper = nk
}

// SetCommonsKeeper sets the CommonsKeeper after depinject.
func (k *Keeper) SetCommonsKeeper(ck types.CommonsKeeper) {
	k.commonsKeeper = ck
}

// SetBlogKeeper sets the BlogKeeper after depinject.
func (k *Keeper) SetBlogKeeper(bk types.BlogKeeper) {
	k.blogKeeper = bk
}

// SetForumKeeper sets the ForumKeeper after depinject.
func (k *Keeper) SetForumKeeper(fk types.ForumKeeper) {
	k.forumKeeper = fk
}

// SetCollectKeeper sets the CollectKeeper after depinject.
func (k *Keeper) SetCollectKeeper(ck types.CollectKeeper) {
	k.collectKeeper = ck
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
func (k Keeper) GetBlogKeeper() types.BlogKeeper { return k.blogKeeper }

// GetForumKeeper returns the optional forum keeper.
func (k Keeper) GetForumKeeper() types.ForumKeeper { return k.forumKeeper }

// GetCollectKeeper returns the optional collect keeper.
func (k Keeper) GetCollectKeeper() types.CollectKeeper { return k.collectKeeper }

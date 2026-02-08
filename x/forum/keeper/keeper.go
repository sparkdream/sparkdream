package keeper

import (
	"fmt"

	"cosmossdk.io/collections"
	"cosmossdk.io/core/address"
	corestore "cosmossdk.io/core/store"
	"github.com/cosmos/cosmos-sdk/codec"

	"sparkdream/x/forum/types"
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

	bankKeeper            types.BankKeeper
	repKeeper             types.RepKeeper
	commonsKeeper         types.CommonsKeeper
	Post                  collections.Map[uint64, types.Post]
	PostSeq               collections.Sequence
	Category              collections.Map[uint64, types.Category]
	CategorySeq           collections.Sequence
	Tag                   collections.Map[string, types.Tag]
	ReservedTag           collections.Map[string, types.ReservedTag]
	UserRateLimit         collections.Map[string, types.UserRateLimit]
	UserReactionLimit     collections.Map[string, types.UserReactionLimit]
	SentinelActivity      collections.Map[string, types.SentinelActivity]
	HideRecord            collections.Map[uint64, types.HideRecord]
	ThreadLockRecord      collections.Map[uint64, types.ThreadLockRecord]
	ThreadMoveRecord      collections.Map[uint64, types.ThreadMoveRecord]
	PostFlag              collections.Map[uint64, types.PostFlag]
	BountySeq             collections.Sequence
	Bounty                collections.Map[uint64, types.Bounty]
	TagBudgetSeq          collections.Sequence
	TagBudget             collections.Map[uint64, types.TagBudget]
	TagBudgetAwardSeq     collections.Sequence
	TagBudgetAward        collections.Map[uint64, types.TagBudgetAward]
	ThreadMetadata        collections.Map[uint64, types.ThreadMetadata]
	ThreadFollow          collections.Map[string, types.ThreadFollow]
	ThreadFollowCount     collections.Map[uint64, types.ThreadFollowCount]
	ArchivedThread        collections.Map[uint64, types.ArchivedThread]
	ArchiveMetadata       collections.Map[uint64, types.ArchiveMetadata]
	TagReport             collections.Map[string, types.TagReport]
	MemberSalvationStatus collections.Map[string, types.MemberSalvationStatus]
	JuryParticipation     collections.Map[string, types.JuryParticipation]
	MemberReport          collections.Map[string, types.MemberReport]
	MemberWarningSeq      collections.Sequence
	MemberWarning         collections.Map[uint64, types.MemberWarning]
	GovActionAppealSeq    collections.Sequence
	GovActionAppeal       collections.Map[uint64, types.GovActionAppeal]
}

func NewKeeper(
	storeService corestore.KVStoreService,
	cdc codec.Codec,
	addressCodec address.Codec,
	authority []byte,
	bankKeeper types.BankKeeper,
	repKeeper types.RepKeeper,
	commonsKeeper types.CommonsKeeper,
) Keeper {
	if _, err := addressCodec.BytesToString(authority); err != nil {
		panic(fmt.Sprintf("invalid authority address %s: %s", authority, err))
	}

	sb := collections.NewSchemaBuilder(storeService)

	k := Keeper{
		storeService:      storeService,
		cdc:               cdc,
		addressCodec:      addressCodec,
		authority:         authority,
		bankKeeper:        bankKeeper,
		repKeeper:         repKeeper,
		commonsKeeper:     commonsKeeper,
		Params:            collections.NewItem(sb, types.ParamsKey, "params", codec.CollValue[types.Params](cdc)),
		Post:              collections.NewMap(sb, types.PostKey, "post", collections.Uint64Key, codec.CollValue[types.Post](cdc)),
		PostSeq:           collections.NewSequence(sb, types.PostSeqKey, "postSequence"),
		Category:          collections.NewMap(sb, types.CategoryKey, "category", collections.Uint64Key, codec.CollValue[types.Category](cdc)),
		CategorySeq:       collections.NewSequence(sb, types.CategorySeqKey, "categorySequence"),
		Tag:               collections.NewMap(sb, types.TagKey, "tag", collections.StringKey, codec.CollValue[types.Tag](cdc)),
		ReservedTag:       collections.NewMap(sb, types.ReservedTagKey, "reservedTag", collections.StringKey, codec.CollValue[types.ReservedTag](cdc)),
		UserRateLimit:     collections.NewMap(sb, types.UserRateLimitKey, "userRateLimit", collections.StringKey, codec.CollValue[types.UserRateLimit](cdc)),
		UserReactionLimit: collections.NewMap(sb, types.UserReactionLimitKey, "userReactionLimit", collections.StringKey, codec.CollValue[types.UserReactionLimit](cdc)),
		SentinelActivity:  collections.NewMap(sb, types.SentinelActivityKey, "sentinelActivity", collections.StringKey, codec.CollValue[types.SentinelActivity](cdc)),
		HideRecord:        collections.NewMap(sb, types.HideRecordKey, "hideRecord", collections.Uint64Key, codec.CollValue[types.HideRecord](cdc)),
		ThreadLockRecord:  collections.NewMap(sb, types.ThreadLockRecordKey, "threadLockRecord", collections.Uint64Key, codec.CollValue[types.ThreadLockRecord](cdc)),
		ThreadMoveRecord:  collections.NewMap(sb, types.ThreadMoveRecordKey, "threadMoveRecord", collections.Uint64Key, codec.CollValue[types.ThreadMoveRecord](cdc)),
		PostFlag:          collections.NewMap(sb, types.PostFlagKey, "postFlag", collections.Uint64Key, codec.CollValue[types.PostFlag](cdc)),
		Bounty:            collections.NewMap(sb, types.BountyKey, "bounty", collections.Uint64Key, codec.CollValue[types.Bounty](cdc)),
		BountySeq:         collections.NewSequence(sb, types.BountyCountKey, "bountySequence"),
		TagBudget:         collections.NewMap(sb, types.TagBudgetKey, "tagBudget", collections.Uint64Key, codec.CollValue[types.TagBudget](cdc)),
		TagBudgetSeq:      collections.NewSequence(sb, types.TagBudgetCountKey, "tagBudgetSequence"),
		TagBudgetAward:    collections.NewMap(sb, types.TagBudgetAwardKey, "tagBudgetAward", collections.Uint64Key, codec.CollValue[types.TagBudgetAward](cdc)),
		TagBudgetAwardSeq: collections.NewSequence(sb, types.TagBudgetAwardCountKey, "tagBudgetAwardSequence"),
		ThreadMetadata:    collections.NewMap(sb, types.ThreadMetadataKey, "threadMetadata", collections.Uint64Key, codec.CollValue[types.ThreadMetadata](cdc)), ThreadFollow: collections.NewMap(sb, types.ThreadFollowKey, "threadFollow", collections.StringKey, codec.CollValue[types.ThreadFollow](cdc)), ThreadFollowCount: collections.NewMap(sb, types.ThreadFollowCountKey, "threadFollowCount", collections.Uint64Key, codec.CollValue[types.ThreadFollowCount](cdc)), ArchivedThread: collections.NewMap(sb, types.ArchivedThreadKey, "archivedThread", collections.Uint64Key, codec.CollValue[types.ArchivedThread](cdc)), ArchiveMetadata: collections.NewMap(sb, types.ArchiveMetadataKey, "archiveMetadata", collections.Uint64Key, codec.CollValue[types.ArchiveMetadata](cdc)), TagReport: collections.NewMap(sb, types.TagReportKey, "tagReport", collections.StringKey, codec.CollValue[types.TagReport](cdc)), MemberSalvationStatus: collections.NewMap(sb, types.MemberSalvationStatusKey, "memberSalvationStatus", collections.StringKey, codec.CollValue[types.MemberSalvationStatus](cdc)), JuryParticipation: collections.NewMap(sb, types.JuryParticipationKey, "juryParticipation", collections.StringKey, codec.CollValue[types.JuryParticipation](cdc)), MemberReport: collections.NewMap(sb, types.MemberReportKey, "memberReport", collections.StringKey, codec.CollValue[types.MemberReport](cdc)), MemberWarning: collections.NewMap(sb, types.MemberWarningKey, "memberWarning", collections.Uint64Key, codec.CollValue[types.MemberWarning](cdc)),
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

// GetAuthority returns the module's authority.
func (k Keeper) GetAuthority() []byte {
	return k.authority
}

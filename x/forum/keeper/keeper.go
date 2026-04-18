package keeper

import (
	"context"
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
	UserRateLimit         collections.Map[string, types.UserRateLimit]
	UserReactionLimit     collections.Map[string, types.UserReactionLimit]
	SentinelActivity      collections.Map[string, types.SentinelActivity]
	HideRecord            collections.Map[uint64, types.HideRecord]
	ThreadLockRecord      collections.Map[uint64, types.ThreadLockRecord]
	ThreadMoveRecord      collections.Map[uint64, types.ThreadMoveRecord]
	PostFlag              collections.Map[uint64, types.PostFlag]
	BountySeq             collections.Sequence
	Bounty                collections.Map[uint64, types.Bounty]
	ThreadMetadata        collections.Map[uint64, types.ThreadMetadata]
	ThreadFollow          collections.Map[string, types.ThreadFollow]
	ThreadFollowCount     collections.Map[uint64, types.ThreadFollowCount]
	ArchiveMetadata       collections.Map[uint64, types.ArchiveMetadata]
	MemberSalvationStatus collections.Map[string, types.MemberSalvationStatus]
	JuryParticipation     collections.Map[string, types.JuryParticipation]
	MemberReport          collections.Map[string, types.MemberReport]
	MemberWarningSeq      collections.Sequence
	MemberWarning         collections.Map[uint64, types.MemberWarning]
	GovActionAppealSeq    collections.Sequence
	GovActionAppeal       collections.Map[uint64, types.GovActionAppeal]
	ExpirationQueue       collections.KeySet[collections.Pair[int64, uint64]]
	PostVote              collections.KeySet[collections.Pair[uint64, string]]
	ActiveBountyByThread  collections.Map[uint64, uint64]
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
		UserRateLimit:     collections.NewMap(sb, types.UserRateLimitKey, "userRateLimit", collections.StringKey, codec.CollValue[types.UserRateLimit](cdc)),
		UserReactionLimit: collections.NewMap(sb, types.UserReactionLimitKey, "userReactionLimit", collections.StringKey, codec.CollValue[types.UserReactionLimit](cdc)),
		SentinelActivity:  collections.NewMap(sb, types.SentinelActivityKey, "sentinelActivity", collections.StringKey, codec.CollValue[types.SentinelActivity](cdc)),
		HideRecord:        collections.NewMap(sb, types.HideRecordKey, "hideRecord", collections.Uint64Key, codec.CollValue[types.HideRecord](cdc)),
		ThreadLockRecord:  collections.NewMap(sb, types.ThreadLockRecordKey, "threadLockRecord", collections.Uint64Key, codec.CollValue[types.ThreadLockRecord](cdc)),
		ThreadMoveRecord:  collections.NewMap(sb, types.ThreadMoveRecordKey, "threadMoveRecord", collections.Uint64Key, codec.CollValue[types.ThreadMoveRecord](cdc)),
		PostFlag:          collections.NewMap(sb, types.PostFlagKey, "postFlag", collections.Uint64Key, codec.CollValue[types.PostFlag](cdc)),
		Bounty:            collections.NewMap(sb, types.BountyKey, "bounty", collections.Uint64Key, codec.CollValue[types.Bounty](cdc)),
		BountySeq:         collections.NewSequence(sb, types.BountyCountKey, "bountySequence"),
		ThreadMetadata:    collections.NewMap(sb, types.ThreadMetadataKey, "threadMetadata", collections.Uint64Key, codec.CollValue[types.ThreadMetadata](cdc)), ThreadFollow: collections.NewMap(sb, types.ThreadFollowKey, "threadFollow", collections.StringKey, codec.CollValue[types.ThreadFollow](cdc)), ThreadFollowCount: collections.NewMap(sb, types.ThreadFollowCountKey, "threadFollowCount", collections.Uint64Key, codec.CollValue[types.ThreadFollowCount](cdc)), ArchiveMetadata: collections.NewMap(sb, types.ArchiveMetadataKey, "archiveMetadata", collections.Uint64Key, codec.CollValue[types.ArchiveMetadata](cdc)), MemberSalvationStatus: collections.NewMap(sb, types.MemberSalvationStatusKey, "memberSalvationStatus", collections.StringKey, codec.CollValue[types.MemberSalvationStatus](cdc)), JuryParticipation: collections.NewMap(sb, types.JuryParticipationKey, "juryParticipation", collections.StringKey, codec.CollValue[types.JuryParticipation](cdc)), MemberReport: collections.NewMap(sb, types.MemberReportKey, "memberReport", collections.StringKey, codec.CollValue[types.MemberReport](cdc)), MemberWarning: collections.NewMap(sb, types.MemberWarningKey, "memberWarning", collections.Uint64Key, codec.CollValue[types.MemberWarning](cdc)),
		MemberWarningSeq:   collections.NewSequence(sb, types.MemberWarningCountKey, "memberWarningSequence"),
		GovActionAppeal:    collections.NewMap(sb, types.GovActionAppealKey, "govActionAppeal", collections.Uint64Key, codec.CollValue[types.GovActionAppeal](cdc)),
		GovActionAppealSeq: collections.NewSequence(sb, types.GovActionAppealCountKey, "govActionAppealSequence"),
		ExpirationQueue: collections.NewKeySet(
			sb,
			types.ExpirationQueueKey,
			"expiration_queue",
			collections.PairKeyCodec(collections.Int64Key, collections.Uint64Key),
		),
		PostVote: collections.NewKeySet(
			sb,
			types.PostVoteKey,
			"post_vote",
			collections.PairKeyCodec(collections.Uint64Key, collections.StringKey),
		),
		ActiveBountyByThread: collections.NewMap(sb, types.ActiveBountyByThreadKey, "activeBountyByThread", collections.Uint64Key, collections.Uint64Value),
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

// HasPost returns true if a post (or reply, which is a post with ParentId > 0) exists.
func (k Keeper) HasPost(ctx context.Context, id uint64) bool {
	has, _ := k.Post.Has(ctx, id)
	return has
}

// HasCategory lets x/season's ForumKeeper interface distinguish this keeper
// from x/blog's in depinject.
func (k Keeper) HasCategory(ctx context.Context, id uint64) bool {
	if k.commonsKeeper == nil {
		return false
	}
	return k.commonsKeeper.HasCategory(ctx, id)
}

package keeper

import (
	"fmt"

	"cosmossdk.io/collections"
	"cosmossdk.io/core/address"
	corestore "cosmossdk.io/core/store"
	"github.com/cosmos/cosmos-sdk/codec"

	"sparkdream/x/collect/types"
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

	// External keepers
	bankKeeper    types.BankKeeper
	repKeeper     types.RepKeeper
	commonsKeeper types.CommonsKeeper

	// Primary storage
	Collection    collections.Map[uint64, types.Collection]
	CollectionSeq collections.Sequence

	Item    collections.Map[uint64, types.Item]
	ItemSeq collections.Sequence

	// Collaborator keyed by composite: "{collectionID}/{address}"
	Collaborator collections.Map[string, types.Collaborator]

	Curator collections.Map[string, types.Curator]

	CurationReview    collections.Map[uint64, types.CurationReview]
	CurationReviewSeq collections.Sequence

	CurationSummary collections.Map[uint64, types.CurationSummary]

	SponsorshipRequest collections.Map[uint64, types.SponsorshipRequest]

	// Content moderation storage
	Flag                   collections.Map[string, types.CollectionFlag]
	FlagReviewQueue        collections.KeySet[collections.Pair[int32, uint64]]
	FlagExpiry             collections.KeySet[collections.Pair[int64, string]]
	HideRecord             collections.Map[uint64, types.HideRecord]
	HideRecordSeq          collections.Sequence
	HideRecordByTarget     collections.KeySet[collections.Pair[string, uint64]]
	HideRecordExpiry       collections.KeySet[collections.Pair[int64, uint64]]
	Endorsement            collections.Map[uint64, types.Endorsement]
	EndorsementStakeExpiry collections.KeySet[collections.Pair[int64, uint64]]
	EndorsementPending     collections.KeySet[collections.Pair[int64, uint64]]
	ReactionDedup          collections.Map[string, uint32]
	ReactionLimit          collections.Map[string, uint32]
	CollectionsByStatus    collections.KeySet[collections.Pair[int32, uint64]]

	// Secondary indexes for efficient queries
	CollectionsByOwner  collections.KeySet[collections.Pair[string, uint64]]
	CollectionsByExpiry collections.KeySet[collections.Pair[int64, uint64]]

	ItemsByCollection collections.KeySet[collections.Pair[uint64, uint64]]
	ItemsByOwner      collections.KeySet[collections.Pair[string, uint64]]

	CollaboratorReverse collections.KeySet[collections.Pair[string, uint64]]

	CurationReviewsByCollection collections.KeySet[collections.Pair[uint64, uint64]]
	CurationReviewsByCurator    collections.KeySet[collections.Pair[string, uint64]]

	SponsorshipRequestsByExpiry collections.KeySet[collections.Pair[int64, uint64]]

	// External keepers (optional)
	forumKeeper types.ForumKeeper
}

func NewKeeper(
	storeService corestore.KVStoreService,
	cdc codec.Codec,
	addressCodec address.Codec,
	authority []byte,

	bankKeeper types.BankKeeper,
	repKeeper types.RepKeeper,
	commonsKeeper types.CommonsKeeper,
	forumKeeper types.ForumKeeper,
) Keeper {
	if _, err := addressCodec.BytesToString(authority); err != nil {
		panic(fmt.Sprintf("invalid authority address %s: %s", authority, err))
	}

	sb := collections.NewSchemaBuilder(storeService)

	k := Keeper{
		storeService:  storeService,
		cdc:           cdc,
		addressCodec:  addressCodec,
		authority:     authority,
		bankKeeper:    bankKeeper,
		repKeeper:     repKeeper,
		commonsKeeper: commonsKeeper,
		forumKeeper:   forumKeeper,

		Params: collections.NewItem(sb, types.ParamsKey, "params", codec.CollValue[types.Params](cdc)),

		// Primary storage
		Collection:    collections.NewMap(sb, types.CollectionKey, "collection", collections.Uint64Key, codec.CollValue[types.Collection](cdc)),
		CollectionSeq: collections.NewSequence(sb, types.CollectionSeqKey, "collectionSequence"),

		Item:    collections.NewMap(sb, types.ItemKey, "item", collections.Uint64Key, codec.CollValue[types.Item](cdc)),
		ItemSeq: collections.NewSequence(sb, types.ItemSeqKey, "itemSequence"),

		Collaborator: collections.NewMap(sb, types.CollaboratorKey, "collaborator", collections.StringKey, codec.CollValue[types.Collaborator](cdc)),

		Curator: collections.NewMap(sb, types.CuratorKey, "curator", collections.StringKey, codec.CollValue[types.Curator](cdc)),

		CurationReview:    collections.NewMap(sb, types.CurationReviewKey, "curationReview", collections.Uint64Key, codec.CollValue[types.CurationReview](cdc)),
		CurationReviewSeq: collections.NewSequence(sb, types.CurationReviewSeqKey, "curationReviewSequence"),

		CurationSummary: collections.NewMap(sb, types.CurationSummaryKey, "curationSummary", collections.Uint64Key, codec.CollValue[types.CurationSummary](cdc)),

		SponsorshipRequest: collections.NewMap(sb, types.SponsorshipRequestKey, "sponsorshipRequest", collections.Uint64Key, codec.CollValue[types.SponsorshipRequest](cdc)),

		// Content moderation storage
		Flag: collections.NewMap(sb, types.FlagKey, "flag", collections.StringKey, codec.CollValue[types.CollectionFlag](cdc)),
		FlagReviewQueue: collections.NewKeySet(
			sb, types.FlagReviewQueueKey, "flagReviewQueue",
			collections.PairKeyCodec(collections.Int32Key, collections.Uint64Key),
		),
		FlagExpiry: collections.NewKeySet(
			sb, types.FlagExpiryKey, "flagExpiry",
			collections.PairKeyCodec(collections.Int64Key, collections.StringKey),
		),
		HideRecord:    collections.NewMap(sb, types.HideRecordKey, "hideRecord", collections.Uint64Key, codec.CollValue[types.HideRecord](cdc)),
		HideRecordSeq: collections.NewSequence(sb, types.HideRecordSeqKey, "hideRecordSequence"),
		HideRecordByTarget: collections.NewKeySet(
			sb, types.HideRecordByTargetKey, "hideRecordByTarget",
			collections.PairKeyCodec(collections.StringKey, collections.Uint64Key),
		),
		HideRecordExpiry: collections.NewKeySet(
			sb, types.HideRecordExpiryKey, "hideRecordExpiry",
			collections.PairKeyCodec(collections.Int64Key, collections.Uint64Key),
		),
		Endorsement: collections.NewMap(sb, types.EndorsementKey, "endorsement", collections.Uint64Key, codec.CollValue[types.Endorsement](cdc)),
		EndorsementStakeExpiry: collections.NewKeySet(
			sb, types.EndorsementStakeExpiryKey, "endorsementStakeExpiry",
			collections.PairKeyCodec(collections.Int64Key, collections.Uint64Key),
		),
		EndorsementPending: collections.NewKeySet(
			sb, types.EndorsementPendingKey, "endorsementPending",
			collections.PairKeyCodec(collections.Int64Key, collections.Uint64Key),
		),
		ReactionDedup: collections.NewMap(sb, types.ReactionDedupKey, "reactionDedup", collections.StringKey, collections.Uint32Value),
		ReactionLimit: collections.NewMap(sb, types.ReactionLimitKey, "reactionLimit", collections.StringKey, collections.Uint32Value),
		CollectionsByStatus: collections.NewKeySet(
			sb, types.CollectionsByStatusKey, "collectionsByStatus",
			collections.PairKeyCodec(collections.Int32Key, collections.Uint64Key),
		),

		// Secondary indexes
		CollectionsByOwner: collections.NewKeySet(
			sb, types.CollectionsByOwnerKey, "collectionsByOwner",
			collections.PairKeyCodec(collections.StringKey, collections.Uint64Key),
		),
		CollectionsByExpiry: collections.NewKeySet(
			sb, types.CollectionsByExpiryKey, "collectionsByExpiry",
			collections.PairKeyCodec(collections.Int64Key, collections.Uint64Key),
		),
		ItemsByCollection: collections.NewKeySet(
			sb, types.ItemsByCollectionKey, "itemsByCollection",
			collections.PairKeyCodec(collections.Uint64Key, collections.Uint64Key),
		),
		ItemsByOwner: collections.NewKeySet(
			sb, types.ItemsByOwnerKey, "itemsByOwner",
			collections.PairKeyCodec(collections.StringKey, collections.Uint64Key),
		),
		CollaboratorReverse: collections.NewKeySet(
			sb, types.CollaboratorReverseKey, "collaboratorReverse",
			collections.PairKeyCodec(collections.StringKey, collections.Uint64Key),
		),
		CurationReviewsByCollection: collections.NewKeySet(
			sb, types.CurationReviewsByCollectionKey, "curationReviewsByCollection",
			collections.PairKeyCodec(collections.Uint64Key, collections.Uint64Key),
		),
		CurationReviewsByCurator: collections.NewKeySet(
			sb, types.CurationReviewsByCuratorKey, "curationReviewsByCurator",
			collections.PairKeyCodec(collections.StringKey, collections.Uint64Key),
		),
		SponsorshipRequestsByExpiry: collections.NewKeySet(
			sb, types.SponsorshipRequestsByExpiryKey, "sponsorshipRequestsByExpiry",
			collections.PairKeyCodec(collections.Int64Key, collections.Uint64Key),
		),
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

// GetAuthorityString returns the module's authority as a string.
func (k Keeper) GetAuthorityString() string {
	addr, _ := k.addressCodec.BytesToString(k.authority)
	return addr
}

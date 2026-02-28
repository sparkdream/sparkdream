package types

import "cosmossdk.io/collections"

const (
	// ModuleName defines the module name
	ModuleName = "collect"

	// StoreKey defines the primary module store key
	StoreKey = ModuleName

	// GovModuleName duplicates the gov module's name to avoid a dependency with x/gov.
	// It should be synced with the gov module's name if it is ever changed.
	// See: https://github.com/cosmos/cosmos-sdk/blob/v0.52.0-beta.2/x/gov/types/keys.go#L9
	GovModuleName = "gov"
)

// ParamsKey is the prefix to retrieve all Params
var ParamsKey = collections.NewPrefix("p_collect")

// Primary storage keys
var (
	CollectionKey    = collections.NewPrefix("collection/value/")
	CollectionSeqKey = collections.NewPrefix("collection/seq/")

	ItemKey    = collections.NewPrefix("item/value/")
	ItemSeqKey = collections.NewPrefix("item/seq/")

	CollaboratorKey = collections.NewPrefix("collaborator/value/")

	CuratorKey = collections.NewPrefix("curator/value/")

	CurationReviewKey    = collections.NewPrefix("curation_review/value/")
	CurationReviewSeqKey = collections.NewPrefix("curation_review/seq/")

	CurationSummaryKey = collections.NewPrefix("curation_summary/value/")

	SponsorshipRequestKey = collections.NewPrefix("sponsorship_request/value/")
)

// Secondary index keys
var (
	// CollectionsByOwnerKey: (owner, collectionID) - for querying collections by owner
	CollectionsByOwnerKey = collections.NewPrefix("collection/by_owner/")

	// CollectionsByExpiryKey: (expiresAt, collectionID) - for TTL pruning
	CollectionsByExpiryKey = collections.NewPrefix("collection/by_expiry/")

	// ItemsByCollectionKey: (collectionID, position) - items by collection ordered by position
	ItemsByCollectionKey = collections.NewPrefix("item/by_collection/")

	// ItemsByOwnerKey: (collectionOwner, itemID) - items by collection owner
	ItemsByOwnerKey = collections.NewPrefix("item/by_owner/")

	// CollaboratorReverseKey: (address, collectionID) - reverse index for collaborator lookups
	CollaboratorReverseKey = collections.NewPrefix("collaborator/by_address/")

	// CurationReviewsByCollectionKey: (collectionID, reviewID) - reviews by collection
	CurationReviewsByCollectionKey = collections.NewPrefix("curation_review/by_collection/")

	// CurationReviewsByCuratorKey: (curator, reviewID) - reviews by curator
	CurationReviewsByCuratorKey = collections.NewPrefix("curation_review/by_curator/")

	// SponsorshipRequestsByExpiryKey: (expiresAt, collectionID) - for TTL pruning
	SponsorshipRequestsByExpiryKey = collections.NewPrefix("sponsorship_request/by_expiry/")

	// ItemsByOnChainRefKey: (refKey, itemID) - reverse index for OnChainReference lookups
	// refKey is "{module}:{entity_type}:{entity_id}", e.g. "blog:post:42"
	ItemsByOnChainRefKey = collections.NewPrefix("item/by_onchain_ref/")
)

// Content moderation keys
var (
	// FlagKey: (targetType, targetID) → CollectionFlag
	FlagKey = collections.NewPrefix("flag/value/")

	// FlagReviewQueueKey: (targetType, targetID) → empty - index for in_review_queue
	FlagReviewQueueKey = collections.NewPrefix("flag/review/")

	// FlagExpiryKey: (expiresAt, targetType, targetID) → empty - for flag expiration
	FlagExpiryKey = collections.NewPrefix("flag/expiry/")

	// HideRecordKey: (id) → HideRecord
	HideRecordKey = collections.NewPrefix("hide_record/value/")

	// HideRecordSeqKey: next hide record ID sequence
	HideRecordSeqKey = collections.NewPrefix("hide_record/seq/")

	// HideRecordByTargetKey: (targetType, targetID, id) → empty - by-target index
	HideRecordByTargetKey = collections.NewPrefix("hide_record/by_target/")

	// HideRecordExpiryKey: (appealDeadline, id) → empty - for expiry/appeal timeout
	HideRecordExpiryKey = collections.NewPrefix("hide_record/expiry/")

	// EndorsementKey: (collectionID) → Endorsement
	EndorsementKey = collections.NewPrefix("endorsement/value/")

	// EndorsementStakeExpiryKey: (stakeReleaseAt, collectionID) → empty - stake release index
	EndorsementStakeExpiryKey = collections.NewPrefix("endorsement/stake_expiry/")

	// EndorsementPendingKey: (endorsementExpiry, collectionID) → empty - unendorsed auto-prune
	EndorsementPendingKey = collections.NewPrefix("endorsement/pending/")

	// ReactionLimitKey: (address, day) → count - daily reaction counter
	ReactionLimitKey = collections.NewPrefix("reaction/limit/")

	// ReactionDedupKey: (address, targetType, targetID) → uint8 (1=upvote, 2=downvote)
	ReactionDedupKey = collections.NewPrefix("reaction/dedup/")

	// CollectionsByStatusKey: (status, collectionID) → empty - for querying by status
	CollectionsByStatusKey = collections.NewPrefix("collection/by_status/")
)

// Anonymous collection keys (prefix-based, not collections framework)
const (
	AnonCollectionMetaKey = "AnonMeta/collection/"
	AnonNullifierKey      = "AnonNullifier/"
	AnonMgmtKeyIndexKey   = "AnonMgmtKeyIndex/"
)

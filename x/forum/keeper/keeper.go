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

	bankKeeper           types.BankKeeper
	repKeeper            types.RepKeeper
	commonsKeeper        types.CommonsKeeper
	// identityKeeper is late-bound via SetIdentityKeeper from app.go.
	// Used by BondDenom() to resolve the chain's bond denom for federated
	// chains. Panics if unwired — see BondDenom() for rationale.
	identityKeeper       types.IdentityKeeper
	Post                 collections.Map[uint64, types.Post]
	PostSeq              collections.Sequence
	UserRateLimit        collections.Map[string, types.UserRateLimit]
	UserReactionLimit    collections.Map[string, types.UserReactionLimit]
	SentinelActivity     collections.Map[string, types.SentinelActivity]
	HideRecord           collections.Map[uint64, types.HideRecord]
	ThreadLockRecord     collections.Map[uint64, types.ThreadLockRecord]
	ThreadMoveRecord     collections.Map[uint64, types.ThreadMoveRecord]
	PostFlag             collections.Map[uint64, types.PostFlag]
	BountySeq            collections.Sequence
	Bounty               collections.Map[uint64, types.Bounty]
	ThreadMetadata       collections.Map[uint64, types.ThreadMetadata]
	ThreadFollow         collections.Map[string, types.ThreadFollow]
	ThreadFollowCount    collections.Map[uint64, types.ThreadFollowCount]
	ArchiveMetadata      collections.Map[uint64, types.ArchiveMetadata]
	ExpirationQueue      collections.KeySet[collections.Pair[int64, uint64]]
	PostVote             collections.KeySet[collections.Pair[uint64, string]]
	ActiveBountyByThread collections.Map[uint64, uint64]

	// FORUM-S2-8 secondary indexes for paginated / prefix queries.
	PostsByPinned     collections.KeySet[collections.Pair[uint64, uint64]]
	PostsByUpvotes    collections.KeySet[collections.Pair[uint64, uint64]]
	FollowersByThread collections.KeySet[collections.Pair[uint64, string]]
	ThreadsByFollower collections.KeySet[collections.Pair[string, uint64]]
	BountiesByCreator collections.KeySet[collections.Pair[string, uint64]]
	BountiesByExpiry  collections.KeySet[collections.Pair[int64, uint64]]
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
		ThreadMetadata:    collections.NewMap(sb, types.ThreadMetadataKey, "threadMetadata", collections.Uint64Key, codec.CollValue[types.ThreadMetadata](cdc)),
		ThreadFollow:      collections.NewMap(sb, types.ThreadFollowKey, "threadFollow", collections.StringKey, codec.CollValue[types.ThreadFollow](cdc)),
		ThreadFollowCount: collections.NewMap(sb, types.ThreadFollowCountKey, "threadFollowCount", collections.Uint64Key, codec.CollValue[types.ThreadFollowCount](cdc)),
		ArchiveMetadata:   collections.NewMap(sb, types.ArchiveMetadataKey, "archiveMetadata", collections.Uint64Key, codec.CollValue[types.ArchiveMetadata](cdc)),
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

		PostsByPinned: collections.NewKeySet(sb, types.PostsByPinnedKey, "postsByPinned",
			collections.PairKeyCodec(collections.Uint64Key, collections.Uint64Key)),
		PostsByUpvotes: collections.NewKeySet(sb, types.PostsByUpvotesKey, "postsByUpvotes",
			collections.PairKeyCodec(collections.Uint64Key, collections.Uint64Key)),
		FollowersByThread: collections.NewKeySet(sb, types.FollowersByThreadKey, "followersByThread",
			collections.PairKeyCodec(collections.Uint64Key, collections.StringKey)),
		ThreadsByFollower: collections.NewKeySet(sb, types.ThreadsByFollowerKey, "threadsByFollower",
			collections.PairKeyCodec(collections.StringKey, collections.Uint64Key)),
		BountiesByCreator: collections.NewKeySet(sb, types.BountiesByCreatorKey, "bountiesByCreator",
			collections.PairKeyCodec(collections.StringKey, collections.Uint64Key)),
		BountiesByExpiry: collections.NewKeySet(sb, types.BountiesByExpiryKey, "bountiesByExpiry",
			collections.PairKeyCodec(collections.Int64Key, collections.Uint64Key)),
	}
	schema, err := sb.Build()
	if err != nil {
		panic(err)
	}
	k.Schema = schema

	return k
}

// SetIdentityKeeper late-binds the identity keeper for federated-denom
// resolution. Called from app.go post-depinject.
func (k *Keeper) SetIdentityKeeper(idk types.IdentityKeeper) {
	k.identityKeeper = idk
}

// BondDenom returns the chain's bond denom from the wired identity keeper.
// Panics if identity isn't wired or returns no denom: every call site
// needs a real denom and silently falling back to a hardcoded literal
// re-introduces the mixed-state class of bug we just removed.
func (k Keeper) BondDenom(ctx context.Context) string {
	if k.identityKeeper == nil {
		panic("forum keeper: identityKeeper not wired (call SetIdentityKeeper after depinject)")
	}
	return k.identityKeeper.BondDenom(ctx)
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

// HasPostInCategory reports whether any forum post still references the given
// category. Used by x/commons' MsgDeleteCategory to refuse removal of
// categories that still have content. O(n) over Post — there is no
// PostsByCategory secondary index yet; if delete-category becomes hot or post
// volume grows large, add one and switch to a prefix walk here.
//
// Only ACTIVE posts count: DeletePost / HidePost / archival are soft mutations
// that leave a tombstone in storage, but those posts no longer "reference" the
// category in any user-meaningful sense — counting them would mean an admin
// could never delete a category that ever held a post, even after every post
// has been removed. UNSPECIFIED is genesis-rejected so it should never occur
// at runtime; treating it as a tombstone is the safe default.
//
// INVARIANT (DANGLING-REFERENCE GUARD): because tombstones do NOT block
// category deletion here, every code path that transitions a post FROM a
// terminal status BACK to POST_STATUS_ACTIVE must first check that the parent
// category still exists. Otherwise the resurrected post points at a deleted
// category — a dangling reference. Today only MsgUnarchiveThread can revive
// a tombstone (ARCHIVED → ACTIVE) and it carries that guard. DELETED is
// terminal in code; HIDDEN has no implemented unhide handler (the README's
// MsgUnhidePost is aspirational). Any future revival path MUST apply the
// same `commonsKeeper.HasCategory` check.
func (k Keeper) HasPostInCategory(ctx context.Context, categoryID uint64) (bool, error) {
	found := false
	err := k.Post.Walk(ctx, nil, func(_ uint64, post types.Post) (stop bool, err error) {
		if post.CategoryId != categoryID {
			return false, nil
		}
		if post.Status != types.PostStatus_POST_STATUS_ACTIVE {
			return false, nil
		}
		found = true
		return true, nil
	})
	if err != nil {
		return false, err
	}
	return found, nil
}

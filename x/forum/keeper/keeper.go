package keeper

import (
	"context"
	"fmt"

	"cosmossdk.io/collections"
	"cosmossdk.io/core/address"
	corestore "cosmossdk.io/core/store"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"

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

	bankKeeper    types.BankKeeper
	repKeeper     types.RepKeeper
	commonsKeeper types.CommonsKeeper
	// identityKeeper is late-bound via SetIdentityKeeper from app.go.
	// Used by BondDenom() to resolve the chain's bond denom for federated
	// chains. Panics if unwired — see BondDenom() for rationale.
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
	// RootsByCategory: (category_id, root_post_id) for every stored thread
	// root that is not DELETED, re-keyed when a thread moves
	// (setRootCategory). Derived.
	RootsByCategory collections.KeySet[collections.Pair[uint64, uint64]]
	// LockedRoots indexes locked thread roots for the LockedThreads query
	// (the ThreadLockRecord goes once the appeal window closes). Derived.
	LockedRoots collections.KeySet[uint64]
	// StakeSlashQueue: post_id -> post author for posts whose conviction
	// stakes are still being slashed, a bounded batch per block (FRM-26).
	StakeSlashQueue collections.Map[uint64, string]

	// Membership-driven promotion indexes (see promotion_queue.go).
	EphemeralByAuthor collections.KeySet[collections.Pair[string, uint64]]
	PromotionQueue    collections.Map[string, int64]

	// ProposalAutoConfirmQueue: (fire_at, thread_id) — time-ordered queue of
	// pending sentinel accepted-reply proposals; drained by the EndBlocker
	// (see msg_server_mark_accepted_reply.go / abci.go).
	ProposalAutoConfirmQueue collections.KeySet[collections.Pair[int64, uint64]]

	// ProposalCountByThreadSentinel: (thread_id, sentinel) -> count of
	// accepted-reply proposals that sentinel has made on that thread. Enforces
	// the per-sentinel-per-thread proposal cap (see proposeAcceptedReply).
	ProposalCountByThreadSentinel collections.Map[collections.Pair[uint64, string], uint64]

	// Post conviction-stake state (see post_conviction.go).
	PostConvictionStake          collections.Map[uint64, types.PostConvictionStake]
	PostConvictionStakeSeq       collections.Sequence
	PostConvictionStakesByPost   collections.KeySet[collections.Pair[uint64, uint64]]
	PostConvictionStakesByStaker collections.KeySet[collections.Pair[string, uint64]]
	// ForumRepEpochCounter: (author, tag) -> per-epoch accumulated rep, used
	// by AccruePostConvictions to enforce the per-(author, tag, epoch) cap.
	ForumRepEpochCounter collections.Map[collections.Pair[string, string], types.ForumRepEpochCounter]
	// PostConvictionAccrualCursor holds the stake_id where the next
	// AccruePostConvictions pass should resume. Round-robin progress that
	// prevents the per-block cap from starving high-id stakes when the
	// active set exceeds maxAccrualPerBlock.
	PostConvictionAccrualCursor collections.Item[uint64]

	// Lifecycle indexes (derived; rebuilt in InitGenesis). See types/keys.go.
	PostsByRoot          collections.KeySet[collections.Pair[uint64, uint64]]
	HideRecordExpiry     collections.KeySet[collections.Pair[int64, uint64]]
	LockRecordExpiry     collections.KeySet[collections.Pair[int64, uint64]]
	MoveRecordExpiry     collections.KeySet[collections.Pair[int64, uint64]]
	ReleasedStakesByPost collections.KeySet[collections.Pair[uint64, uint64]]
	ReleasedStakeGc      collections.KeySet[collections.Pair[int64, uint64]]
	// TagPruneQueue: removed tag -> next post id to scan (see
	// PruneTagReferences).
	TagPruneQueue collections.Map[string, uint64]
	// late holds the keepers wired after depinject (app.go Set*Keeper).
	// It is a pointer so the AppModule's value copy of the keeper sees
	// them too (docs/development-conventions.md, AppModule value-copy bug).
	late *lateKeepers
}

// lateKeepers are the dependencies wired after construction; shared by
// every copy of the Keeper value.
type lateKeepers struct {
	seasonKeeper   types.SeasonKeeper
	identityKeeper types.IdentityKeeper
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
		late:              &lateKeepers{},
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
		RootsByCategory: collections.NewKeySet(sb, types.RootsByCategoryKey, "rootsByCategory",
			collections.PairKeyCodec(collections.Uint64Key, collections.Uint64Key)),
		StakeSlashQueue: collections.NewMap(sb, types.StakeSlashQueueKey, "stakeSlashQueue",
			collections.Uint64Key, collections.StringValue),
		LockedRoots: collections.NewKeySet(sb, types.LockedRootsKey, "lockedRoots", collections.Uint64Key),
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

		EphemeralByAuthor: collections.NewKeySet(sb, types.EphemeralByAuthorKey, "ephemeralByAuthor",
			collections.PairKeyCodec(collections.StringKey, collections.Uint64Key)),
		PromotionQueue: collections.NewMap(sb, types.PromotionQueueKey, "promotionQueue",
			collections.StringKey, collections.Int64Value),
		ProposalCountByThreadSentinel: collections.NewMap(sb, types.ProposalCountByThreadSentinelKey, "proposalCountByThreadSentinel",
			collections.PairKeyCodec(collections.Uint64Key, collections.StringKey), collections.Uint64Value),
		ProposalAutoConfirmQueue: collections.NewKeySet(sb, types.ProposalAutoConfirmQueueKey, "proposalAutoConfirmQueue",
			collections.PairKeyCodec(collections.Int64Key, collections.Uint64Key)),

		PostConvictionStake: collections.NewMap(sb, types.PostConvictionStakeKey, "postConvictionStake",
			collections.Uint64Key, codec.CollValue[types.PostConvictionStake](cdc)),
		PostConvictionStakeSeq: collections.NewSequence(sb, types.PostConvictionStakeSeqKey, "postConvictionStakeSequence"),
		PostConvictionStakesByPost: collections.NewKeySet(sb, types.PostConvictionStakesByPostKey, "postConvictionStakesByPost",
			collections.PairKeyCodec(collections.Uint64Key, collections.Uint64Key)),
		PostConvictionStakesByStaker: collections.NewKeySet(sb, types.PostConvictionStakesByStakerKey, "postConvictionStakesByStaker",
			collections.PairKeyCodec(collections.StringKey, collections.Uint64Key)),
		ForumRepEpochCounter: collections.NewMap(sb, types.ForumRepEpochCounterKey, "forumRepEpochCounter",
			collections.PairKeyCodec(collections.StringKey, collections.StringKey),
			codec.CollValue[types.ForumRepEpochCounter](cdc)),
		PostConvictionAccrualCursor: collections.NewItem(sb, types.PostConvictionAccrualCursorKey,
			"postConvictionAccrualCursor", collections.Uint64Value),

		PostsByRoot: collections.NewKeySet(sb, types.PostsByRootKey, "postsByRoot",
			collections.PairKeyCodec(collections.Uint64Key, collections.Uint64Key)),
		HideRecordExpiry: collections.NewKeySet(sb, types.HideRecordExpiryKey, "hideRecordExpiry",
			collections.PairKeyCodec(collections.Int64Key, collections.Uint64Key)),
		LockRecordExpiry: collections.NewKeySet(sb, types.LockRecordExpiryKey, "lockRecordExpiry",
			collections.PairKeyCodec(collections.Int64Key, collections.Uint64Key)),
		MoveRecordExpiry: collections.NewKeySet(sb, types.MoveRecordExpiryKey, "moveRecordExpiry",
			collections.PairKeyCodec(collections.Int64Key, collections.Uint64Key)),
		ReleasedStakesByPost: collections.NewKeySet(sb, types.ReleasedStakesByPostKey, "releasedStakesByPost",
			collections.PairKeyCodec(collections.Uint64Key, collections.Uint64Key)),
		ReleasedStakeGc: collections.NewKeySet(sb, types.ReleasedStakeGcKey, "releasedStakeGc",
			collections.PairKeyCodec(collections.Int64Key, collections.Uint64Key)),
		TagPruneQueue: collections.NewMap(sb, types.TagPruneQueueKey, "tagPruneQueue",
			collections.StringKey, collections.Uint64Value),
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
	k.late.identityKeeper = idk
}

// SetSeasonKeeper late-binds x/season so replies and accepted answers grant
// the author XP. Optional: unwired means no XP.
func (k *Keeper) SetSeasonKeeper(sk types.SeasonKeeper) {
	k.late.seasonKeeper = sk
}

// notifySeason runs one x/season hook best-effort: the forum write has
// already happened, so a hook error is logged and swallowed.
func (k Keeper) notifySeason(ctx context.Context, hook string, fn func(sk types.SeasonKeeper) error) {
	if k.late.seasonKeeper == nil {
		return
	}
	if err := fn(k.late.seasonKeeper); err != nil {
		sdk.UnwrapSDKContext(ctx).Logger().Error("x/season hook failed (non-fatal)", "hook", hook, "error", err)
	}
}

// BondDenom returns the chain's bond denom from the wired identity keeper.
// Panics if identity isn't wired or returns no denom: every call site
// needs a real denom and silently falling back to a hardcoded literal
// re-introduces the mixed-state class of bug we just removed.
func (k Keeper) BondDenom(ctx context.Context) string {
	if k.late.identityKeeper == nil {
		panic("forum keeper: identityKeeper not wired (call SetIdentityKeeper after depinject)")
	}
	return k.late.identityKeeper.BondDenom(ctx)
}

// GetAuthority returns the module's authority.
func (k Keeper) GetAuthority() []byte {
	return k.authority
}

// HasPost returns true if a post (or reply, which is a post with ParentId > 0) exists.
func (k Keeper) HasPost(ctx context.Context, id uint64) bool {
	has, getErr := k.Post.Has(ctx, id)
	if getErr != nil {
		sdk.UnwrapSDKContext(ctx).Logger().Error("Has failed", "err", getErr)
	}
	return has
}

// LivePostCreatedAt returns the creation time (unix seconds) of a forum post
// that exists and is ACTIVE (not hidden, deleted or archived), and false
// otherwise. Used by x/season's retro-PGF nomination checks.
func (k Keeper) LivePostCreatedAt(ctx context.Context, id uint64) (int64, bool) {
	post, err := k.Post.Get(ctx, id)
	if err != nil || post.Status != types.PostStatus_POST_STATUS_ACTIVE {
		return 0, false
	}
	return post.CreatedAt, true
}

// HasCategory lets x/season's ForumKeeper interface distinguish this keeper
// from x/blog's in depinject.
func (k Keeper) HasCategory(ctx context.Context, id uint64) bool {
	if k.commonsKeeper == nil {
		return false
	}
	return k.commonsKeeper.HasCategory(ctx, id)
}

// HasPostInCategory reports whether the given category still holds a live
// thread. Used by x/commons' MsgDeleteCategory to refuse removal of
// categories that still have content. It walks the category's thread roots
// (RootsByCategory) rather than every post (FRM-33), and the root decides:
// after a move the root is authoritative and its replies keep their
// creation-time category, so a moved thread no longer blocks deleting the
// category it left.
//
// Only ACTIVE roots count: DeletePost / HidePost / archival are soft mutations
// that leave a tombstone in storage, but those posts no longer "reference" the
// category in any user-meaningful sense — counting them would mean an admin
// could never delete a category that ever held a post, even after every post
// has been removed. UNSPECIFIED is genesis-rejected so it should never occur
// at runtime; treating it as a tombstone is the safe default. DELETED is
// terminal, so a deleted root also leaves RootsByCategory and is not walked
// again; HIDDEN and ARCHIVED roots stay (both can come back).
//
// INVARIANT (DANGLING-REFERENCE GUARD): because tombstones do NOT block
// category deletion here, every code path that transitions a post FROM a
// terminal status BACK to POST_STATUS_ACTIVE must first check that the parent
// category still exists. Otherwise the resurrected post points at a deleted
// category — a dangling reference. Today only MsgUnarchiveThread can revive
// a tombstone (ARCHIVED → ACTIVE) and it carries that guard. DELETED is
// terminal in code; MsgUnhidePost (HIDDEN → ACTIVE) carries the same
// check. Any future revival path MUST apply the same
// `commonsKeeper.HasCategory` check.
func (k Keeper) HasPostInCategory(ctx context.Context, categoryID uint64) (bool, error) {
	found := false
	rng := collections.NewPrefixedPairRange[uint64, uint64](categoryID)
	err := k.RootsByCategory.Walk(ctx, rng, func(key collections.Pair[uint64, uint64]) (stop bool, err error) {
		post, err := k.Post.Get(ctx, key.K2())
		if err != nil || post.CategoryId != categoryID || post.Status != types.PostStatus_POST_STATUS_ACTIVE {
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

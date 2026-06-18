package types

import "cosmossdk.io/collections"

const (
	// ModuleName defines the module name
	ModuleName = "forum"

	// StoreKey defines the primary module store key
	StoreKey = ModuleName

	// GovModuleName duplicates the gov module's name to avoid a dependency with x/gov.
	// It should be synced with the gov module's name if it is ever changed.
	// See: https://github.com/cosmos/cosmos-sdk/blob/v0.52.0-beta.2/x/gov/types/keys.go#L9
	GovModuleName = "gov"
)

// ParamsKey is the prefix to retrieve all Params
var ParamsKey = collections.NewPrefix("p_forum")

var (
	BountyKey      = collections.NewPrefix("bounty/value/")
	BountyCountKey = collections.NewPrefix("bounty/count/")
)

// Sequence keys for auto-incrementing IDs
var (
	PostSeqKey = collections.NewPrefix("post/seq/")
)

// ExpirationQueueKey is the prefix for the ephemeral post expiration queue
var ExpirationQueueKey = collections.NewPrefix("expiration_queue/")

// EphemeralByAuthorKey indexes still-ephemeral posts by (author, post_id) so
// the EndBlocker promotion drain can locate a queued author's pending content
// without scanning ExpirationQueue. Forum unifies root posts and replies under
// one Post record, so a single key set covers both. Maintained in lockstep
// with ExpirationQueue.
var EphemeralByAuthorKey = collections.NewPrefix("ephemeral_by_author/")

// ProposalAutoConfirmQueueKey holds the time-ordered queue of pending sentinel
// accepted-reply proposals awaiting author action. Key = (fire_at unix seconds,
// thread_id); the EndBlocker walks entries with fire_at <= now and either
// auto-confirms the proposal or grants the one-time inactivity extension.
// Transient/derived — rebuilt from ThreadMetadata pending proposals at genesis
// import rather than carried as a genesis field.
var ProposalAutoConfirmQueueKey = collections.NewPrefix("proposal_auto_confirm_queue/")

// PromotionQueueKey holds the set of authors with ephemeral posts awaiting
// eager promotion to permanent (enqueued when the author becomes a member).
// Key = author bech32 string, value = enqueue block-height (8B big-endian) for
// telemetry only. Drained by the EndBlocker; transient (not persisted to
// genesis).
var PromotionQueueKey = collections.NewPrefix("promotion_queue/")

// FORUM-S2-8 secondary indexes — replace unbounded full-store walks with
// prefix lookups maintained on every relevant write path. See keeper write
// sites (upvote/downvote, pin/unpin, follow/unfollow, bounty lifecycle) for
// where these get inserted/removed.
var (
	// PostsByPinned: (categoryID, postID) — for PinnedPosts query.
	PostsByPinnedKey = collections.NewPrefix("idx/posts_pinned/")
	// PostsByUpvotes: (upvoteCount, postID), iterated in descending order
	// to find the top post. Maintained on upvote/downvote/post-status change.
	PostsByUpvotesKey = collections.NewPrefix("idx/posts_upvotes/")
	// FollowersByThread: (threadID, follower) — for ThreadFollowers query.
	FollowersByThreadKey = collections.NewPrefix("idx/followers_thread/")
	// ThreadsByFollower: (follower, threadID) — for UserFollowedThreads query.
	ThreadsByFollowerKey = collections.NewPrefix("idx/threads_follower/")
	// BountiesByCreator: (creator, bountyID) — for UserBounties query.
	BountiesByCreatorKey = collections.NewPrefix("idx/bounties_creator/")
	// BountiesByExpiry: (expiresAt, bountyID) — for BountyExpiringSoon query.
	BountiesByExpiryKey = collections.NewPrefix("idx/bounties_expiry/")
)

// Post conviction-stake collections (see x/forum/keeper/post_conviction.go
// for accrual and slash semantics).
var (
	// PostConvictionStake: (stake_id) -> PostConvictionStake.
	PostConvictionStakeKey    = collections.NewPrefix("post_conviction/value/")
	PostConvictionStakeSeqKey = collections.NewPrefix("post_conviction/seq/")
	// PostConvictionStakesByPost indexes active stakes for a post for the
	// EndBlocker accrual loop and ExpireHiddenPosts slash path.
	// (post_id, stake_id).
	PostConvictionStakesByPostKey = collections.NewPrefix("idx/post_conviction_by_post/")
	// PostConvictionStakesByStaker indexes a staker's open stakes for queries
	// and bulk release. (staker, stake_id).
	PostConvictionStakesByStakerKey = collections.NewPrefix("idx/post_conviction_by_staker/")
	// ForumRepEpochCounter: (author, tag) -> ForumRepEpochCounter. Used by
	// the post-conviction accrual loop to enforce
	// max_forum_rep_per_tag_per_epoch.
	ForumRepEpochCounterKey = collections.NewPrefix("forum_rep_epoch/")
	// PostConvictionAccrualCursor: stake_id at which the next AccruePostConvictions
	// pass should resume. Persists round-robin progress across blocks so the
	// per-block cap can't starve high-id stakes.
	PostConvictionAccrualCursorKey = collections.NewPrefix("post_conviction/accrual_cursor/")
)

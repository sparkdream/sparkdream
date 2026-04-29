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

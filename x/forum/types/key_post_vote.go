package types

import "cosmossdk.io/collections"

// PostVoteKey is the prefix for tracking individual post votes (postId, voterAddress).
// Prevents duplicate voting on posts.
var PostVoteKey = collections.NewPrefix("postVote/")

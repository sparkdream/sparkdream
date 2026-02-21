package types

import "cosmossdk.io/collections"

// AnonymousVoteKey is the prefix to retrieve all AnonymousVote
var AnonymousVoteKey = collections.NewPrefix("anonymousVote/value/")

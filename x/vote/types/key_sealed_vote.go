package types

import "cosmossdk.io/collections"

// SealedVoteKey is the prefix to retrieve all SealedVote
var SealedVoteKey = collections.NewPrefix("sealedVote/value/")

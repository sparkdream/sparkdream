package types

import "cosmossdk.io/collections"

// UserReactionLimitKey is the prefix to retrieve all UserReactionLimit
var UserReactionLimitKey = collections.NewPrefix("userReactionLimit/value/")

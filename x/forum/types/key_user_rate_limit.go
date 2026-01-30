package types

import "cosmossdk.io/collections"

// UserRateLimitKey is the prefix to retrieve all UserRateLimit
var UserRateLimitKey = collections.NewPrefix("userRateLimit/value/")

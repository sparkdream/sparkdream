package types

import "cosmossdk.io/collections"

var (
	MemberWarningKey      = collections.NewPrefix("memberWarning/value/")
	MemberWarningCountKey = collections.NewPrefix("memberWarning/count/")
)

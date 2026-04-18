package types

import "cosmossdk.io/collections"

var (
	GovActionAppealKey      = collections.NewPrefix("govActionAppeal/value/")
	GovActionAppealCountKey = collections.NewPrefix("govActionAppeal/count/")
)

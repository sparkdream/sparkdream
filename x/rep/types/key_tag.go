package types

import "cosmossdk.io/collections"

var (
	// TagKey: tag name -> Tag
	TagKey = collections.NewPrefix("tag/value/")

	// ReservedTagKey: tag name -> ReservedTag
	ReservedTagKey = collections.NewPrefix("reservedtag/value/")
)

package types

import "cosmossdk.io/collections"

var (
	// CategoryKey is the prefix to retrieve all Category records.
	CategoryKey = collections.NewPrefix("category/value/")
	// CategorySeqKey is the prefix for the category auto-increment sequence.
	CategorySeqKey = collections.NewPrefix("category/seq/")
)

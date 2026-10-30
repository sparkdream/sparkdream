package types

import "cosmossdk.io/collections"

const (
	// ModuleName defines the module name.
	ModuleName = "guardian"

	// StoreKey defines the primary module store key.
	StoreKey = ModuleName

	// RouterKey is the message route for slashing.
	RouterKey = ModuleName
)

// LastBlocksPerYearChangeKey stores the time of the last accepted
// mint blocks_per_year change (its cooldown).
var LastBlocksPerYearChangeKey = collections.NewPrefix(0)

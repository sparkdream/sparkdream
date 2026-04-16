package types

import "cosmossdk.io/collections"

// ActiveBountyByThreadKey is the prefix for the secondary index mapping thread ID to active bounty ID.
// Used for O(1) duplicate bounty checks instead of full table scans.
var ActiveBountyByThreadKey = collections.NewPrefix("activeBountyByThread/")

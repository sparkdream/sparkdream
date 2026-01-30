package types

import "cosmossdk.io/collections"

// SeasonSnapshotKey is the prefix to retrieve all SeasonSnapshot
var SeasonSnapshotKey = collections.NewPrefix("seasonSnapshot/value/")

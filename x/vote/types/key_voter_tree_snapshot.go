package types

import "cosmossdk.io/collections"

// VoterTreeSnapshotKey is the prefix to retrieve all VoterTreeSnapshot
var VoterTreeSnapshotKey = collections.NewPrefix("voterTreeSnapshot/value/")

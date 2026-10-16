package types

import "cosmossdk.io/collections"

// AchievementKey is the prefix to retrieve all Achievement
var AchievementKey = collections.NewPrefix("achievement/value/")

// AchievementsByRequirementKey indexes achievement ids by requirement type
// (int32 enum value) so a hook that bumped one counter reads only the
// definitions that counter can satisfy.
var AchievementsByRequirementKey = collections.NewPrefix("achievement/byreq/")

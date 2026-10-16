package types

import "cosmossdk.io/collections"

// TitleKey is the prefix to retrieve all Title
var TitleKey = collections.NewPrefix("title/value/")

// TitlesByRequirementKey indexes title ids by requirement type, like
// AchievementsByRequirementKey.
var TitlesByRequirementKey = collections.NewPrefix("title/byreq/")

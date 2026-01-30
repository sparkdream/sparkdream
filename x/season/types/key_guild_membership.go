package types

import "cosmossdk.io/collections"

// GuildMembershipKey is the prefix to retrieve all GuildMembership
var GuildMembershipKey = collections.NewPrefix("guildMembership/value/")

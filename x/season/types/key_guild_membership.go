package types

import "cosmossdk.io/collections"

// GuildMembershipKey is the prefix to retrieve all GuildMembership
var GuildMembershipKey = collections.NewPrefix("guildMembership/value/")

// GuildMembersKey indexes GuildMembership by guild: (guild_id, member) for
// active memberships, guild 0 for the rest (SEA-24/SEA-28).
var GuildMembersKey = collections.NewPrefix("guildMembership/byGuild/")

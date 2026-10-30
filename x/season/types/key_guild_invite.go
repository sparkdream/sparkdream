package types

import "cosmossdk.io/collections"

// GuildInviteKey is the prefix to retrieve all GuildInvite
var GuildInviteKey = collections.NewPrefix("guildInvite/value/")

// GuildInvitesByInviteeKey indexes GuildInvite by invitee:
// (invitee, "<guild_id>:<invitee>") (SEA-28).
var GuildInvitesByInviteeKey = collections.NewPrefix("guildInvite/byInvitee/")

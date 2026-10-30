package keeper

import (
	"context"
	"errors"
	"strings"

	"cosmossdk.io/collections"
	"cosmossdk.io/collections/indexes"

	"sparkdream/x/season/types"
)

// GuildMembershipIndexes are the secondary indexes of GuildMembership.
type GuildMembershipIndexes struct {
	// ByGuild: (guild_id, member) for an active membership, (0, member) for
	// a past one, so a guild's members are one prefix walk (SEA-28).
	ByGuild *indexes.Multi[uint64, string, types.GuildMembership]
}

func (i GuildMembershipIndexes) IndexesList() []collections.Index[string, types.GuildMembership] {
	return []collections.Index[string, types.GuildMembership]{i.ByGuild}
}

// activeGuildOf returns the guild a membership record is active in, or 0.
func activeGuildOf(m types.GuildMembership) uint64 {
	if m.LeftEpoch != 0 {
		return 0
	}
	return m.GuildId
}

func newGuildMembershipIndexes(sb *collections.SchemaBuilder) GuildMembershipIndexes {
	return GuildMembershipIndexes{
		ByGuild: indexes.NewMulti(sb, types.GuildMembersKey, "guild_membership_by_guild",
			collections.Uint64Key, collections.StringKey,
			func(_ string, v types.GuildMembership) (uint64, error) {
				return activeGuildOf(v), nil
			}),
	}
}

// GuildInviteIndexes are the secondary indexes of GuildInvite.
type GuildInviteIndexes struct {
	// ByInvitee: (invitee, "<guild_id>:<invitee>"), so a member's invites
	// are one prefix walk (SEA-28).
	ByInvitee *indexes.Multi[string, string, types.GuildInvite]
}

func (i GuildInviteIndexes) IndexesList() []collections.Index[string, types.GuildInvite] {
	return []collections.Index[string, types.GuildInvite]{i.ByInvitee}
}

func newGuildInviteIndexes(sb *collections.SchemaBuilder) GuildInviteIndexes {
	return GuildInviteIndexes{
		ByInvitee: indexes.NewMulti(sb, types.GuildInvitesByInviteeKey, "guild_invite_by_invitee",
			collections.StringKey, collections.StringKey,
			func(_ string, v types.GuildInvite) (string, error) {
				return v.GuildInvitee, nil
			}),
	}
}

// GuildIndexes are the secondary indexes of Guild.
type GuildIndexes struct {
	// ByName: (lowercase name, id) for a live guild, ("", id) for a
	// dissolved one, so the name-uniqueness check is one prefix read
	// (SEA-28).
	ByName *indexes.Multi[string, uint64, types.Guild]
	// ActiveCount counts ACTIVE guilds, so SeasonStats reads one value
	// instead of scanning every guild (SEA-35).
	ActiveCount *GuildActiveCounter
}

func (i GuildIndexes) IndexesList() []collections.Index[uint64, types.Guild] {
	return []collections.Index[uint64, types.Guild]{i.ByName, i.ActiveCount}
}

// GuildActiveCounter is a counting index over the Guild map: every write
// and removal adjusts the number of guilds whose status is ACTIVE. Being an
// index, it is maintained by every Guild.Set / Guild.Remove (including
// InitGenesis), so it can never drift from the guild records.
type GuildActiveCounter struct {
	count collections.Item[uint64]
}

func newGuildActiveCounter(sb *collections.SchemaBuilder) *GuildActiveCounter {
	return &GuildActiveCounter{
		count: collections.NewItem(sb, types.GuildActiveCountKey, "guild_active_count", collections.Uint64Value),
	}
}

func guildIsActive(g types.Guild) bool {
	return g.Status == types.GuildStatus_GUILD_STATUS_ACTIVE
}

// Get returns the number of ACTIVE guilds.
func (c *GuildActiveCounter) Get(ctx context.Context) (uint64, error) {
	n, err := c.count.Get(ctx)
	if errors.Is(err, collections.ErrNotFound) {
		return 0, nil
	}
	return n, err
}

func (c *GuildActiveCounter) add(ctx context.Context, delta int) error {
	n, err := c.Get(ctx)
	if err != nil {
		return err
	}
	switch {
	case delta > 0:
		n++
	case n > 0:
		n--
	}
	// Zero is stored as absence, as it is after an import of no active
	// guilds (the counter is rebuilt by the map writes at InitGenesis).
	if n == 0 {
		return c.count.Remove(ctx)
	}
	return c.count.Set(ctx, n)
}

// Reference implements collections.Index.
func (c *GuildActiveCounter) Reference(ctx context.Context, _ uint64, newValue types.Guild, lazyOldValue func() (types.Guild, error)) error {
	wasActive := false
	old, err := lazyOldValue()
	switch {
	case err == nil:
		wasActive = guildIsActive(old)
	case !errors.Is(err, collections.ErrNotFound):
		return err
	}
	isActive := guildIsActive(newValue)
	switch {
	case isActive && !wasActive:
		return c.add(ctx, 1)
	case !isActive && wasActive:
		return c.add(ctx, -1)
	}
	return nil
}

// Unreference implements collections.Index.
func (c *GuildActiveCounter) Unreference(ctx context.Context, _ uint64, lazyOldValue func() (types.Guild, error)) error {
	old, err := lazyOldValue()
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil
		}
		return err
	}
	if guildIsActive(old) {
		return c.add(ctx, -1)
	}
	return nil
}

// guildNameKey is the ByName index key of a guild.
func guildNameKey(g types.Guild) string {
	if g.Status == types.GuildStatus_GUILD_STATUS_DISSOLVED {
		return ""
	}
	return strings.ToLower(g.Name)
}

func newGuildIndexes(sb *collections.SchemaBuilder) GuildIndexes {
	return GuildIndexes{
		ByName: indexes.NewMulti(sb, types.GuildsByNameKey, "guild_by_name",
			collections.StringKey, collections.Uint64Key,
			func(_ uint64, v types.Guild) (string, error) {
				return guildNameKey(v), nil
			}),
		ActiveCount: newGuildActiveCounter(sb),
	}
}

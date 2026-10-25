package keeper

import (
	"context"
	"fmt"
	"strings"

	sdk "github.com/cosmos/cosmos-sdk/types"

	reptypes "sparkdream/x/rep/types"
	"sparkdream/x/season/types"
)

// MigrationHooks implements x/rep's MemberMigrationHooks for x/season
// (member-key-migration plan §8, season row): the member profile (XP,
// level, titles, achievements, guild), registration, guild membership and
// the guild's founder / officer / pending-invite references move eagerly
// (a member is in at most one guild, so this is bounded), together with the
// open-season XP tracker and quest progress. Snapshots and nominations stay
// lazy and are read through the alias.
type MigrationHooks struct {
	k *Keeper
}

// NewMigrationHooks returns the x/season migration hooks.
func NewMigrationHooks(k *Keeper) MigrationHooks {
	return MigrationHooks{k: k}
}

var _ reptypes.MemberMigrationHooks = MigrationHooks{}

// BeforeMemberMigration refuses a target that already has a season profile
// (freshness, plan §7.1).
func (h MigrationHooks) BeforeMemberMigration(ctx context.Context, from, to sdk.AccAddress, path reptypes.MigrationPath) error {
	if h.k.HasMemberProfile(ctx, to.String()) {
		return fmt.Errorf("x/season: %s already has a member profile", to.String())
	}
	if _, err := h.k.MemberRegistration.Get(ctx, to.String()); err == nil {
		return fmt.Errorf("x/season: %s is already registered", to.String())
	}
	return nil
}

// AfterMemberMigration moves the bounded per-member season state.
func (h MigrationHooks) AfterMemberMigration(ctx context.Context, from, to sdk.AccAddress, path reptypes.MigrationPath) error {
	k := h.k
	fromStr, toStr := from.String(), to.String()

	var guildID uint64
	if profile, err := k.MemberProfile.Get(ctx, fromStr); err == nil {
		guildID = profile.GuildId
		if err := k.MemberProfile.Remove(ctx, fromStr); err != nil {
			return err
		}
		profile.Address = toStr
		if err := k.MemberProfile.Set(ctx, toStr, profile); err != nil {
			return err
		}
	}
	if reg, err := k.MemberRegistration.Get(ctx, fromStr); err == nil {
		if err := k.MemberRegistration.Remove(ctx, fromStr); err != nil {
			return err
		}
		reg.Member = toStr
		if err := k.MemberRegistration.Set(ctx, toStr, reg); err != nil {
			return err
		}
	}
	if membership, err := k.GuildMembership.Get(ctx, fromStr); err == nil {
		if err := k.GuildMembership.Remove(ctx, fromStr); err != nil {
			return err
		}
		membership.Member = toStr
		if err := k.GuildMembership.Set(ctx, toStr, membership); err != nil {
			return err
		}
		if guildID == 0 {
			guildID = membership.GuildId
		}
	}
	if guildID != 0 {
		if guild, err := k.Guild.Get(ctx, guildID); err == nil {
			changed := false
			if guild.Founder == fromStr {
				guild.Founder = toStr
				changed = true
			}
			for i, o := range guild.Officers {
				if o == fromStr {
					guild.Officers[i] = toStr
					changed = true
				}
			}
			if changed {
				if err := k.Guild.Set(ctx, guildID, guild); err != nil {
					return err
				}
			}
		}
	}
	// Pending guild invites addressed to the old address ("<guild>:<addr>")
	// and guilds listing it among pending_invites. Bounded by the number of
	// guilds that invited this one member.
	var inviteKeys []string
	if err := k.GuildInvite.Walk(ctx, nil, func(key string, inv types.GuildInvite) (bool, error) {
		if inv.GuildInvitee == fromStr {
			inviteKeys = append(inviteKeys, key)
		}
		return false, nil
	}); err != nil {
		return err
	}
	for _, key := range inviteKeys {
		inv, err := k.GuildInvite.Get(ctx, key)
		if err != nil {
			continue
		}
		if err := k.GuildInvite.Remove(ctx, key); err != nil {
			return err
		}
		inv.GuildInvitee = toStr
		newKey := key
		if i := strings.Index(key, ":"); i >= 0 {
			newKey = key[:i+1] + toStr
		}
		if err := k.GuildInvite.Set(ctx, newKey, inv); err != nil {
			return err
		}
		if i := strings.Index(key, ":"); i >= 0 {
			var gid uint64
			if _, err := fmt.Sscanf(key[:i], "%d", &gid); err == nil {
				if guild, err := k.Guild.Get(ctx, gid); err == nil {
					for j, p := range guild.PendingInvites {
						if p == fromStr {
							guild.PendingInvites[j] = toStr
						}
					}
					if err := k.Guild.Set(ctx, gid, guild); err != nil {
						return err
					}
				}
			}
		}
	}

	// Open-epoch XP tracker ("<addr>:<epoch>") and quest progress
	// ("<addr>:<quest>"); both carry the address in the key.
	epoch := k.GetCurrentEpoch(ctx)
	xpKey := epochXPTrackerKey(fromStr, epoch)
	if tracker, err := k.EpochXpTracker.Get(ctx, xpKey); err == nil {
		if err := k.EpochXpTracker.Remove(ctx, xpKey); err != nil {
			return err
		}
		tracker.MemberEpoch = epochXPTrackerKey(toStr, epoch)
		if err := k.EpochXpTracker.Set(ctx, tracker.MemberEpoch, tracker); err != nil {
			return err
		}
	}
	prefix := fromStr + ":"
	var questKeys []string
	if err := k.MemberQuestProgress.Walk(ctx, nil, func(key string, _ types.MemberQuestProgress) (bool, error) {
		if strings.HasPrefix(key, prefix) {
			questKeys = append(questKeys, key)
		}
		return false, nil
	}); err != nil {
		return err
	}
	for _, key := range questKeys {
		progress, err := k.MemberQuestProgress.Get(ctx, key)
		if err != nil {
			continue
		}
		if err := k.MemberQuestProgress.Remove(ctx, key); err != nil {
			return err
		}
		progress.MemberQuest = toStr + ":" + strings.TrimPrefix(key, prefix)
		if err := k.MemberQuestProgress.Set(ctx, progress.MemberQuest, progress); err != nil {
			return err
		}
	}

	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent("season_profile_migrated",
		sdk.NewAttribute("from", fromStr),
		sdk.NewAttribute("to", toStr),
		sdk.NewAttribute("guild_id", fmt.Sprintf("%d", guildID)),
	))
	return nil
}

package keeper

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"cosmossdk.io/collections"
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
	// SEA-29: on the KEY path an open display-name case (reported, maybe
	// appealed) waits: the member migrates once it is resolved. RECOVERY and
	// COUNCIL are never blocked by from-side state (REP-R-101): anyone can
	// file a report, so a veto here let a thief or griefer burn the victim's
	// recovery commitments one by one. On those paths the open case moves to
	// the new address in AfterMemberMigration.
	if path == reptypes.MigrationPath_MIGRATION_PATH_KEY {
		if m, err := h.k.DisplayNameModeration.Get(ctx, from.String()); err == nil && m.Active {
			return fmt.Errorf("x/season: %s has an unresolved display-name moderation", from.String())
		}
	}
	if _, err := h.k.DisplayNameModeration.Get(ctx, to.String()); err == nil {
		return fmt.Errorf("x/season: %s already has a display-name moderation record", to.String())
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
		if profile.Username != "" {
			if err := k.UsernameOwner.Set(ctx, strings.ToLower(profile.Username), toStr); err != nil {
				return err
			}
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
	// and guilds listing it among pending_invites, found through the
	// ByInvitee index (SEA-28): bounded by the guilds that invited this one
	// member.
	var inviteKeys []string
	if err := k.GuildInvite.Indexes.ByInvitee.Walk(ctx,
		collections.NewPrefixedPairRange[string, string](fromStr),
		func(_ string, key string) (bool, error) {
			inviteKeys = append(inviteKeys, key)
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
		gid := inv.GuildId
		if gid == 0 {
			gid = extractGuildIdFromInviteKey(key)
			inv.GuildId = gid
		}
		if err := k.GuildInvite.Set(ctx, guildInviteKey(gid, toStr), inv); err != nil {
			return err
		}
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

	// The display-name record follows the member: a closed one carries the
	// blocked names (SEA-23), an open one (RECOVERY / COUNCIL only; KEY
	// vetoed it) its queue entries and the report stake, which is keyed by
	// the member's address. The appeal stake stays keyed by its challenge id
	// and is released through the alias.
	if err := k.moveDisplayNameModeration(ctx, fromStr, toStr); err != nil {
		return err
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
	// Only this member's keys: "<addr>:" up to "<addr>;" (';' follows ':'),
	// never the whole progress table (SEA-28).
	questRange := new(collections.Range[string]).StartInclusive(prefix).EndExclusive(fromStr + ";")
	if err := k.MemberQuestProgress.Walk(ctx, questRange, func(key string, _ types.MemberQuestProgress) (bool, error) {
		questKeys = append(questKeys, key)
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

// moveDisplayNameModeration re-keys from's display-name moderation record to
// to, with its queue entries and, for an open case, the report stake.
func (k Keeper) moveDisplayNameModeration(ctx context.Context, from, to string) error {
	m, err := k.DisplayNameModeration.Get(ctx, from)
	if errors.Is(err, collections.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if m.Active {
		oldID := fmt.Sprintf("dn:%s:%d", m.Member, m.ModeratedAt)
		if stake, sErr := k.DisplayNameReportStake.Get(ctx, oldID); sErr == nil {
			if err := k.DisplayNameReportStake.Remove(ctx, oldID); err != nil {
				return err
			}
			stake.ChallengeId = fmt.Sprintf("dn:%s:%d", to, m.ModeratedAt)
			if err := k.DisplayNameReportStake.Set(ctx, stake.ChallengeId, stake); err != nil {
				return err
			}
		} else if !errors.Is(sErr, collections.ErrNotFound) {
			return sErr
		}
	}
	// Writing an inactive placeholder drops from's queue entries; then the
	// record itself goes.
	if err := k.SetDisplayNameModeration(ctx, from, types.DisplayNameModeration{Member: from}); err != nil {
		return err
	}
	if err := k.DisplayNameModeration.Remove(ctx, from); err != nil {
		return err
	}
	m.Member = to
	return k.SetDisplayNameModeration(ctx, to, m)
}

package keeper

import (
	"fmt"
	"strings"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/season/types"
)

// RegisterInvariants registers the x/season invariants (SEA-36). The chain
// does not wire x/crisis; app/invariants_test.go runs them against every
// e2e post-setup snapshot.
func RegisterInvariants(ir sdk.InvariantRegistry, k Keeper) {
	ir.RegisterRoute(types.ModuleName, "guild-membership", GuildMembershipInvariant(k))
	ir.RegisterRoute(types.ModuleName, "guild-invites", GuildInviteInvariant(k))
	ir.RegisterRoute(types.ModuleName, "nomination-stakes", NominationStakeInvariant(k))
	ir.RegisterRoute(types.ModuleName, "display-name-moderation", DisplayNameModerationInvariant(k))
}

// GuildMembershipInvariant: every active membership points at an existing,
// non-dissolved guild and agrees with the member's profile, and each
// guild's GuildMemberCount equals its active memberships (SEA-24).
func GuildMembershipInvariant(k Keeper) sdk.Invariant {
	return func(ctx sdk.Context) (string, bool) {
		var msgs []string
		active := map[uint64]uint64{}
		if err := k.GuildMembership.Walk(ctx, nil, func(member string, m types.GuildMembership) (bool, error) {
			gid := activeGuildOf(m)
			if gid == 0 {
				return false, nil
			}
			active[gid]++
			g, err := k.Guild.Get(ctx, gid)
			switch {
			case err != nil:
				msgs = append(msgs, fmt.Sprintf("%s is a member of missing guild %d", member, gid))
			case g.Status == types.GuildStatus_GUILD_STATUS_DISSOLVED:
				msgs = append(msgs, fmt.Sprintf("%s is a member of dissolved guild %d", member, gid))
			}
			if p, err := k.MemberProfile.Get(ctx, member); err == nil && p.GuildId != gid {
				msgs = append(msgs, fmt.Sprintf("%s: membership guild %d, profile guild %d", member, gid, p.GuildId))
			}
			return false, nil
		}); err != nil {
			return fmt.Sprintf("GuildMembership.Walk failed: %v", err), true
		}
		if err := k.Guild.Walk(ctx, nil, func(id uint64, g types.Guild) (bool, error) {
			count, err := k.GuildMemberCount.Get(ctx, id)
			if err != nil {
				count = 0
			}
			if count != active[id] {
				msgs = append(msgs, fmt.Sprintf("guild %d: member counter %d, active memberships %d", id, count, active[id]))
			}
			return false, nil
		}); err != nil {
			return fmt.Sprintf("Guild.Walk failed: %v", err), true
		}
		broken := len(msgs) > 0
		return sdk.FormatInvariant(types.ModuleName, "guild-membership",
			fmt.Sprintf("%d inconsistencies\n%s", len(msgs), strings.Join(msgs, "\n"))), broken
	}
}

// GuildInviteInvariant: every invite is stored under "<guild_id>:<invitee>"
// built from its own fields (SEA-25).
func GuildInviteInvariant(k Keeper) sdk.Invariant {
	return func(ctx sdk.Context) (string, bool) {
		var msgs []string
		if err := k.GuildInvite.Walk(ctx, nil, func(key string, inv types.GuildInvite) (bool, error) {
			if want := guildInviteKey(inv.GuildId, inv.GuildInvitee); key != want {
				msgs = append(msgs, fmt.Sprintf("invite stored under %q, fields give %q", key, want))
			}
			return false, nil
		}); err != nil {
			return fmt.Sprintf("GuildInvite.Walk failed: %v", err), true
		}
		broken := len(msgs) > 0
		return sdk.FormatInvariant(types.ModuleName, "guild-invites",
			fmt.Sprintf("%d inconsistencies\n%s", len(msgs), strings.Join(msgs, "\n"))), broken
	}
}

// NominationStakeInvariant: every stake references an existing nomination
// and each nomination's total_staked equals the sum of its stakes.
func NominationStakeInvariant(k Keeper) sdk.Invariant {
	return func(ctx sdk.Context) (string, bool) {
		var msgs []string
		sums := map[uint64]math.LegacyDec{}
		if err := k.NominationStake.Walk(ctx, nil, func(key string, st types.NominationStake) (bool, error) {
			if has, _ := k.Nomination.Has(ctx, st.NominationId); !has {
				msgs = append(msgs, fmt.Sprintf("stake %s references missing nomination %d", key, st.NominationId))
				return false, nil
			}
			cur, ok := sums[st.NominationId]
			if !ok {
				cur = math.LegacyZeroDec()
			}
			sums[st.NominationId] = cur.Add(st.Amount)
			return false, nil
		}); err != nil {
			return fmt.Sprintf("NominationStake.Walk failed: %v", err), true
		}
		if err := k.Nomination.Walk(ctx, nil, func(id uint64, nom types.Nomination) (bool, error) {
			sum, ok := sums[id]
			if !ok {
				// Stakes are returned (and removed) at the transition while
				// total_staked keeps the historical figure.
				return false, nil
			}
			if nom.TotalStaked.IsNil() || !nom.TotalStaked.Equal(sum) {
				msgs = append(msgs, fmt.Sprintf("nomination %d: total_staked %s, stakes sum %s", id, nom.TotalStaked, sum))
			}
			return false, nil
		}); err != nil {
			return fmt.Sprintf("Nomination.Walk failed: %v", err), true
		}
		broken := len(msgs) > 0
		return sdk.FormatInvariant(types.ModuleName, "nomination-stakes",
			fmt.Sprintf("%d inconsistencies\n%s", len(msgs), strings.Join(msgs, "\n"))), broken
	}
}

// DisplayNameModerationInvariant: every report or appeal stake belongs to
// an active moderation, and an active appealed moderation is in the appeal
// queue (SEA-23).
func DisplayNameModerationInvariant(k Keeper) sdk.Invariant {
	return func(ctx sdk.Context) (string, bool) {
		var msgs []string
		openReports := map[string]bool{}
		openAppeals := map[string]bool{}
		if err := k.DisplayNameModeration.Walk(ctx, nil, func(member string, m types.DisplayNameModeration) (bool, error) {
			if !m.Active {
				return false, nil
			}
			openReports[fmt.Sprintf("dn:%s:%d", m.Member, m.ModeratedAt)] = true
			if m.AppealChallengeId != "" {
				openAppeals[m.AppealChallengeId] = true
			}
			return false, nil
		}); err != nil {
			return fmt.Sprintf("DisplayNameModeration.Walk failed: %v", err), true
		}
		if err := k.DisplayNameReportStake.Walk(ctx, nil, func(id string, _ types.DisplayNameReportStake) (bool, error) {
			if !openReports[id] {
				msgs = append(msgs, fmt.Sprintf("report stake %s has no active moderation", id))
			}
			return false, nil
		}); err != nil {
			return fmt.Sprintf("DisplayNameReportStake.Walk failed: %v", err), true
		}
		if err := k.DisplayNameAppealStake.Walk(ctx, nil, func(id string, _ types.DisplayNameAppealStake) (bool, error) {
			if !openAppeals[id] {
				msgs = append(msgs, fmt.Sprintf("appeal stake %s has no active appealed moderation", id))
			}
			return false, nil
		}); err != nil {
			return fmt.Sprintf("DisplayNameAppealStake.Walk failed: %v", err), true
		}
		broken := len(msgs) > 0
		return sdk.FormatInvariant(types.ModuleName, "display-name-moderation",
			fmt.Sprintf("%d inconsistencies\n%s", len(msgs), strings.Join(msgs, "\n"))), broken
	}
}

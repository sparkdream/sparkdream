package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/season/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// LeaveGuild allows a member to leave their current guild.
// Founders cannot leave - they must transfer ownership or dissolve.
func (k msgServer) LeaveGuild(ctx context.Context, msg *types.MsgLeaveGuild) (*types.MsgLeaveGuildResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Check maintenance mode
	if k.IsInMaintenanceMode(ctx) {
		return nil, types.ErrMaintenanceMode
	}

	// Get member profile
	profile, err := k.MemberProfile.Get(ctx, msg.Creator)
	if err != nil {
		return nil, errorsmod.Wrap(types.ErrProfileNotFound, "member profile not found")
	}

	// Check member is in a guild
	if profile.GuildId == 0 {
		return nil, types.ErrNotInGuild
	}

	guildID := profile.GuildId

	// Get the guild
	guild, err := k.Guild.Get(ctx, guildID)
	if err != nil {
		return nil, errorsmod.Wrapf(types.ErrGuildNotFound, "guild %d not found", guildID)
	}

	// Check if founder - founders cannot leave
	if guild.Founder == msg.Creator {
		return nil, types.ErrCannotLeaveAsFounder
	}

	// Remove from officers if applicable
	if k.IsGuildOfficer(ctx, guildID, msg.Creator) {
		newOfficers := make([]string, 0, len(guild.Officers)-1)
		for _, officer := range guild.Officers {
			if officer != msg.Creator {
				newOfficers = append(newOfficers, officer)
			}
		}
		guild.Officers = newOfficers
		if err := k.Guild.Set(ctx, guildID, guild); err != nil {
			return nil, errorsmod.Wrap(err, "failed to update guild")
		}
	}

	// Update member profile
	profile.GuildId = 0
	profile.LastActiveEpoch = k.GetCurrentEpoch(ctx)
	if err := k.MemberProfile.Set(ctx, msg.Creator, profile); err != nil {
		return nil, errorsmod.Wrap(err, "failed to update profile")
	}

	// Update membership record
	membership, _ := k.GuildMembership.Get(ctx, msg.Creator)
	membership.GuildId = 0
	membership.LeftEpoch = k.GetCurrentEpoch(ctx)
	if err := k.GuildMembership.Set(ctx, msg.Creator, membership); err != nil {
		return nil, errorsmod.Wrap(err, "failed to update membership")
	}

	// Update active member counter
	k.DecrementGuildMemberCount(ctx, guildID)

	// Check if guild drops below minimum - freeze if so
	params, _ := k.Params.Get(ctx)
	memberCount := k.GetGuildMemberCount(ctx, guildID)
	if memberCount < uint64(params.MinGuildMembers) && guild.Status == types.GuildStatus_GUILD_STATUS_ACTIVE {
		guild.Status = types.GuildStatus_GUILD_STATUS_FROZEN
		if err := k.Guild.Set(ctx, guildID, guild); err != nil {
			return nil, errorsmod.Wrap(err, "failed to freeze guild")
		}

		sdkCtx.EventManager().EmitEvent(
			sdk.NewEvent(
				"guild_frozen",
				sdk.NewAttribute("guild_id", fmt.Sprintf("%d", guildID)),
				sdk.NewAttribute("reason", "below_minimum_members"),
			),
		)
	}

	// Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"guild_left",
			sdk.NewAttribute("guild_id", fmt.Sprintf("%d", guildID)),
			sdk.NewAttribute("member", msg.Creator),
		),
	)

	return &types.MsgLeaveGuildResponse{}, nil
}

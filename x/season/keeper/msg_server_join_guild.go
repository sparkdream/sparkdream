package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/season/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// JoinGuild allows a member to join a guild (if not invite-only or has invite).
func (k msgServer) JoinGuild(ctx context.Context, msg *types.MsgJoinGuild) (*types.MsgJoinGuildResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Check maintenance mode
	if k.IsInMaintenanceMode(ctx) {
		return nil, types.ErrMaintenanceMode
	}

	// Get the guild
	guild, err := k.Guild.Get(ctx, msg.GuildId)
	if err != nil {
		return nil, errorsmod.Wrapf(types.ErrGuildNotFound, "guild %d not found", msg.GuildId)
	}

	// Check guild status
	if guild.Status == types.GuildStatus_GUILD_STATUS_DISSOLVED {
		return nil, types.ErrGuildDissolved
	}
	if guild.Status == types.GuildStatus_GUILD_STATUS_FROZEN {
		return nil, types.ErrGuildFrozen
	}

	// Get member profile
	profile, err := k.MemberProfile.Get(ctx, msg.Creator)
	if err != nil {
		return nil, errorsmod.Wrap(types.ErrProfileNotFound, "member profile not found")
	}

	// Check not already in a guild
	if profile.GuildId != 0 {
		return nil, types.ErrAlreadyInGuild
	}

	// Check guild capacity
	params, _ := k.Params.Get(ctx)
	memberCount := k.GetGuildMemberCount(ctx, msg.GuildId)
	if memberCount >= uint64(params.MaxGuildMembers) {
		return nil, types.ErrGuildFull
	}

	// Check guild hop cooldown
	membership, err := k.GuildMembership.Get(ctx, msg.Creator)
	currentEpoch := k.GetCurrentEpoch(ctx)
	if err == nil && membership.LeftEpoch > 0 {
		epochsSinceLeft := currentEpoch - membership.LeftEpoch
		if uint64(epochsSinceLeft) < params.GuildHopCooldownEpochs {
			return nil, errorsmod.Wrapf(types.ErrGuildHopCooldown,
				"must wait %d more epochs", params.GuildHopCooldownEpochs-uint64(epochsSinceLeft))
		}
	}

	// Check max guilds per season
	if err == nil && membership.GuildsJoinedThisSeason >= uint64(params.MaxGuildsPerSeason) {
		return nil, types.ErrMaxGuildsPerSeason
	}

	// Check if invite-only
	if guild.InviteOnly {
		// Check for pending invite
		if !k.HasPendingGuildInvite(ctx, msg.GuildId, msg.Creator) {
			return nil, types.ErrGuildInviteOnly
		}
		// Remove from pending invites
		k.removePendingInvite(ctx, &guild, msg.Creator)
	}

	// Update member profile
	profile.GuildId = msg.GuildId
	profile.LastActiveEpoch = currentEpoch
	if err := k.MemberProfile.Set(ctx, msg.Creator, profile); err != nil {
		return nil, errorsmod.Wrap(err, "failed to update profile")
	}

	// Create/update membership record
	newMembership := types.GuildMembership{
		Member:                 msg.Creator,
		GuildId:                msg.GuildId,
		JoinedEpoch:            currentEpoch,
		LeftEpoch:              0,
		GuildsJoinedThisSeason: 1,
	}
	if err == nil {
		newMembership.GuildsJoinedThisSeason = membership.GuildsJoinedThisSeason + 1
	}
	if err := k.GuildMembership.Set(ctx, msg.Creator, newMembership); err != nil {
		return nil, errorsmod.Wrap(err, "failed to save membership")
	}

	// Update active member counter
	k.IncrementGuildMemberCount(ctx, msg.GuildId)

	// Save updated guild (if we removed invite)
	if guild.InviteOnly {
		if err := k.Guild.Set(ctx, msg.GuildId, guild); err != nil {
			return nil, errorsmod.Wrap(err, "failed to update guild")
		}
	}

	// Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"guild_joined",
			sdk.NewAttribute("guild_id", fmt.Sprintf("%d", msg.GuildId)),
			sdk.NewAttribute("member", msg.Creator),
		),
	)

	return &types.MsgJoinGuildResponse{}, nil
}

// removePendingInvite removes an invite from the guild's pending list
func (k msgServer) removePendingInvite(ctx context.Context, guild *types.Guild, invitee string) {
	newInvites := make([]string, 0, len(guild.PendingInvites)-1)
	for _, invite := range guild.PendingInvites {
		if invite != invitee {
			newInvites = append(newInvites, invite)
		}
	}
	guild.PendingInvites = newInvites

	// Also delete the invite record
	key := fmt.Sprintf("%d:%s", guild.Id, invitee)
	_ = k.GuildInvite.Remove(ctx, key)
}

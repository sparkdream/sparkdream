package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/season/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// AcceptGuildInvite accepts a pending guild invite.
func (k msgServer) AcceptGuildInvite(ctx context.Context, msg *types.MsgAcceptGuildInvite) (*types.MsgAcceptGuildInviteResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Check maintenance mode
	if k.IsInMaintenanceMode(ctx) {
		return nil, types.ErrMaintenanceMode
	}

	// Get the invite
	key := fmt.Sprintf("%d:%s", msg.GuildId, msg.Creator)
	invite, err := k.GuildInvite.Get(ctx, key)
	if err != nil {
		return nil, types.ErrNoGuildInvite
	}

	// Check invite not expired
	currentEpoch := k.GetCurrentEpoch(ctx)
	if invite.ExpiresEpoch > 0 && currentEpoch > invite.ExpiresEpoch {
		// Clean up expired invite
		_ = k.GuildInvite.Remove(ctx, key)
		return nil, types.ErrGuildInviteExpired
	}

	// Get the guild
	guild, err := k.Guild.Get(ctx, msg.GuildId)
	if err != nil {
		return nil, errorsmod.Wrapf(types.ErrGuildNotFound, "guild %d not found", msg.GuildId)
	}

	// Check guild status
	if guild.Status != types.GuildStatus_GUILD_STATUS_ACTIVE {
		if guild.Status == types.GuildStatus_GUILD_STATUS_DISSOLVED {
			return nil, types.ErrGuildDissolved
		}
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

	// Remove the invite
	_ = k.GuildInvite.Remove(ctx, key)

	// Remove from pending invites list
	newInvites := make([]string, 0, len(guild.PendingInvites)-1)
	for _, inv := range guild.PendingInvites {
		if inv != msg.Creator {
			newInvites = append(newInvites, inv)
		}
	}
	guild.PendingInvites = newInvites
	if err := k.Guild.Set(ctx, msg.GuildId, guild); err != nil {
		return nil, errorsmod.Wrap(err, "failed to update guild")
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

	// Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"guild_invite_accepted",
			sdk.NewAttribute("guild_id", fmt.Sprintf("%d", msg.GuildId)),
			sdk.NewAttribute("member", msg.Creator),
			sdk.NewAttribute("inviter", invite.Inviter),
		),
	)

	return &types.MsgAcceptGuildInviteResponse{}, nil
}

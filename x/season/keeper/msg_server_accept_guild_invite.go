package keeper

import (
	"context"
	"errors"
	"fmt"
	commontypes "sparkdream/x/common/types"

	"cosmossdk.io/collections"

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
		if err := commontypes.RemoveIndexed(ctx, k.GuildInvite, key); err != nil {
			return nil, err
		}
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
	params, getErr := k.Params.Get(ctx)
	if getErr != nil && !errors.Is(getErr, collections.ErrNotFound) {
		return nil, getErr
	}
	memberCount := k.GetGuildMemberCount(ctx, msg.GuildId)
	if memberCount >= uint64(params.MaxGuildMembers) {
		return nil, types.ErrGuildFull
	}

	// Check guild hop cooldown
	membership, err := k.GuildMembership.Get(ctx, msg.Creator)
	if err := checkGuildHopCooldown(membership, err == nil, currentEpoch, params.GuildHopCooldownEpochs); err != nil {
		return nil, err
	}

	// Check max guilds per season
	if err == nil && k.guildsJoinedThisSeason(ctx, membership) >= uint64(params.MaxGuildsPerSeason) {
		return nil, types.ErrMaxGuildsPerSeason
	}

	// Remove the invite
	if err := commontypes.RemoveIndexed(ctx, k.GuildInvite, key); err != nil {
		return nil, err
	}

	// Remove from pending invites list
	newInvites := make([]string, 0, len(guild.PendingInvites))
	for _, inv := range guild.PendingInvites {
		if !k.canActAs(ctx, msg.Creator, inv) {
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

	// Create/update membership record and the active member counter
	// (SEA-24: an accepted invite counts toward max_guild_members).
	if err := k.joinGuildMembership(ctx, msg.Creator, msg.GuildId, currentEpoch); err != nil {
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

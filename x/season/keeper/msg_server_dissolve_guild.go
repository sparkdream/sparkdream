package keeper

import (
	"context"
	"errors"
	"fmt"

	commontypes "sparkdream/x/common/types"
	"sparkdream/x/season/types"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// DissolveGuild dissolves a guild. Only the founder can dissolve.
// All members are removed and the guild name is released.
func (k msgServer) DissolveGuild(ctx context.Context, msg *types.MsgDissolveGuild) (*types.MsgDissolveGuildResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Get the guild
	guild, err := k.Guild.Get(ctx, msg.GuildId)
	if err != nil {
		return nil, errorsmod.Wrapf(types.ErrGuildNotFound, "guild %d not found", msg.GuildId)
	}

	// Check guild not already dissolved
	if guild.Status == types.GuildStatus_GUILD_STATUS_DISSOLVED {
		return nil, types.ErrGuildDissolved
	}

	// Verify the creator is the founder
	if !k.canActAs(ctx, msg.Creator, guild.Founder) {
		return nil, types.ErrNotGuildFounder
	}

	// Check guild age (cannot dissolve brand new guild)
	params, getErr := k.Params.Get(ctx)
	if getErr != nil && !errors.Is(getErr, collections.ErrNotFound) {
		return nil, getErr
	}
	currentEpoch := k.GetCurrentEpoch(ctx)
	guildAgeEpochs := currentEpoch - k.BlockToEpoch(ctx, guild.CreatedBlock)
	if commontypes.Saturate[uint64](guildAgeEpochs) < params.MinGuildAgeEpochs {
		return nil, errorsmod.Wrapf(types.ErrGuildTooYoung,
			"guild must be at least %d epochs old", params.MinGuildAgeEpochs)
	}

	// Remove all members from the guild: one walk of the guild's ByGuild
	// index (bounded by max_guild_members), never the whole membership
	// table (SEA-28). The counter ends at zero (SEA-24).
	for _, member := range k.guildMembers(ctx, msg.GuildId) {
		if _, err := k.leaveGuildMembership(ctx, member, currentEpoch); err != nil {
			return nil, errorsmod.Wrap(err, "failed to update membership")
		}
		profile, err := k.MemberProfile.Get(ctx, member)
		if err == nil {
			profile.GuildId = 0
			if err := k.MemberProfile.Set(ctx, member, profile); err != nil {
				return nil, errorsmod.Wrap(err, "failed to update profile")
			}
		}
	}
	if err := k.setGuildMemberCount(ctx, msg.GuildId, 0); err != nil {
		return nil, errorsmod.Wrap(err, "failed to reset member count")
	}

	// Drop the guild's pending invites with it.
	for _, invitee := range guild.PendingInvites {
		if err := k.GuildInvite.Remove(ctx, guildInviteKey(msg.GuildId, invitee)); err != nil && !errors.Is(err, collections.ErrNotFound) {
			return nil, errorsmod.Wrap(err, "failed to remove invite")
		}
	}

	// Mark guild as dissolved
	guild.Status = types.GuildStatus_GUILD_STATUS_DISSOLVED
	guild.Officers = []string{}
	guild.PendingInvites = []string{}

	if err := k.Guild.Set(ctx, msg.GuildId, guild); err != nil {
		return nil, errorsmod.Wrap(err, "failed to update guild")
	}

	// Release guild name via x/name integration
	if err := k.ReleaseName(ctx, guild.Name, guild.Founder); err != nil {
		// Log but don't fail - guild is already dissolved
		sdkCtx.Logger().Error("failed to release guild name", "name", guild.Name, "error", err)
	}

	// Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"guild_dissolved",
			sdk.NewAttribute("guild_id", fmt.Sprintf("%d", msg.GuildId)),
			sdk.NewAttribute("dissolved_by", msg.Creator),
		),
	)

	return &types.MsgDissolveGuildResponse{}, nil
}

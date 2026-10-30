package keeper

import (
	"context"
	"cosmossdk.io/collections"
	"errors"
	"fmt"

	"sparkdream/x/season/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// KickFromGuild kicks a member from the guild.
// Founder or officers can kick members. Founder cannot be kicked.
func (k msgServer) KickFromGuild(ctx context.Context, msg *types.MsgKickFromGuild) (*types.MsgKickFromGuildResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}
	if _, err := k.addressCodec.StringToBytes(msg.Member); err != nil {
		return nil, errorsmod.Wrap(err, "invalid member address")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Get the guild
	guild, err := k.Guild.Get(ctx, msg.GuildId)
	if err != nil {
		return nil, errorsmod.Wrapf(types.ErrGuildNotFound, "guild %d not found", msg.GuildId)
	}

	// Check guild status
	if guild.Status == types.GuildStatus_GUILD_STATUS_DISSOLVED {
		return nil, types.ErrGuildDissolved
	}

	// Verify creator is founder or officer
	if !k.IsGuildFounderOrOfficer(ctx, msg.GuildId, msg.Creator) {
		return nil, types.ErrNotGuildFounderOrOfficer
	}

	// Cannot kick founder
	if k.sameMember(ctx, guild.Founder, msg.Member) {
		return nil, types.ErrCannotKickFounder
	}

	// Cannot kick self
	if k.sameMember(ctx, msg.Creator, msg.Member) {
		return nil, errorsmod.Wrap(types.ErrNotGuildMember, "cannot kick self")
	}

	// Verify member is in the guild
	if !k.IsGuildMember(ctx, msg.GuildId, msg.Member) {
		return nil, types.ErrNotGuildMember
	}

	// Officers cannot kick other officers (only founder can)
	if k.IsGuildOfficer(ctx, msg.GuildId, msg.Member) && !k.canActAs(ctx, msg.Creator, guild.Founder) {
		return nil, errorsmod.Wrap(types.ErrNotGuildFounder, "only founder can kick officers")
	}

	// Remove from officers if applicable
	if k.IsGuildOfficer(ctx, msg.GuildId, msg.Member) {
		newOfficers := make([]string, 0, len(guild.Officers))
		for _, officer := range guild.Officers {
			if !k.sameMember(ctx, officer, msg.Member) {
				newOfficers = append(newOfficers, officer)
			}
		}
		guild.Officers = newOfficers
		if err := k.Guild.Set(ctx, msg.GuildId, guild); err != nil {
			return nil, errorsmod.Wrap(err, "failed to update guild")
		}
	}

	// Update member profile
	profile, err := k.MemberProfile.Get(ctx, msg.Member)
	if err == nil {
		profile.GuildId = 0
		profile.LastActiveEpoch = k.GetCurrentEpoch(ctx)
		if err := k.MemberProfile.Set(ctx, msg.Member, profile); err != nil {
			return nil, err
		}
	}

	// Update membership record and the active member counter (SEA-24)
	if _, err := k.leaveGuildMembership(ctx, msg.Member, k.GetCurrentEpoch(ctx)); err != nil {
		return nil, errorsmod.Wrap(err, "failed to update membership")
	}

	// Check if guild drops below minimum - freeze if so
	params, getErr := k.Params.Get(ctx)
	if getErr != nil && !errors.Is(getErr, collections.ErrNotFound) {
		return nil, getErr
	}
	memberCount := k.GetGuildMemberCount(ctx, msg.GuildId)
	if memberCount < uint64(params.MinGuildMembers) && guild.Status == types.GuildStatus_GUILD_STATUS_ACTIVE {
		guild.Status = types.GuildStatus_GUILD_STATUS_FROZEN
		if err := k.Guild.Set(ctx, msg.GuildId, guild); err != nil {
			return nil, errorsmod.Wrap(err, "failed to freeze guild")
		}

		sdkCtx.EventManager().EmitEvent(
			sdk.NewEvent(
				"guild_frozen",
				sdk.NewAttribute("guild_id", fmt.Sprintf("%d", msg.GuildId)),
				sdk.NewAttribute("reason", "below_minimum_members"),
			),
		)
	}

	// Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"guild_member_kicked",
			sdk.NewAttribute("guild_id", fmt.Sprintf("%d", msg.GuildId)),
			sdk.NewAttribute("member", msg.Member),
			sdk.NewAttribute("kicked_by", msg.Creator),
			sdk.NewAttribute("reason", msg.Reason),
		),
	)

	return &types.MsgKickFromGuildResponse{}, nil
}

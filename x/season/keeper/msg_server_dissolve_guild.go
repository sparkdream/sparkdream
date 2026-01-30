package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/season/types"

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
	if guild.Founder != msg.Creator {
		return nil, types.ErrNotGuildFounder
	}

	// Check guild age (cannot dissolve brand new guild)
	params, _ := k.Params.Get(ctx)
	currentEpoch := k.GetCurrentEpoch(ctx)
	guildAgeEpochs := currentEpoch - k.BlockToEpoch(ctx, guild.CreatedBlock)
	if uint64(guildAgeEpochs) < params.MinGuildAgeEpochs {
		return nil, errorsmod.Wrapf(types.ErrGuildTooYoung,
			"guild must be at least %d epochs old", params.MinGuildAgeEpochs)
	}

	// Remove all members from the guild
	iter, err := k.GuildMembership.Iterate(ctx, nil)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to iterate memberships")
	}
	defer iter.Close()

	for ; iter.Valid(); iter.Next() {
		membership, err := iter.Value()
		if err != nil {
			continue
		}
		if membership.GuildId == msg.GuildId && membership.LeftEpoch == 0 {
			// Update membership
			membership.GuildId = 0
			membership.LeftEpoch = currentEpoch
			if err := k.GuildMembership.Set(ctx, membership.Member, membership); err != nil {
				continue
			}

			// Update member profile
			profile, err := k.MemberProfile.Get(ctx, membership.Member)
			if err == nil {
				profile.GuildId = 0
				_ = k.MemberProfile.Set(ctx, membership.Member, profile)
			}
		}
	}

	// Mark guild as dissolved
	guild.Status = types.GuildStatus_GUILD_STATUS_DISSOLVED
	guild.Officers = []string{}
	guild.PendingInvites = []string{}

	if err := k.Guild.Set(ctx, msg.GuildId, guild); err != nil {
		return nil, errorsmod.Wrap(err, "failed to update guild")
	}

	// TODO: Release guild name via x/name
	// k.nameKeeper.ReleaseName(ctx, guild.Name)

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

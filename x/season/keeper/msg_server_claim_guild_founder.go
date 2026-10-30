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

// ClaimGuildFounder allows a member to claim founder status of a frozen guild.
// Only available when the guild is frozen (founder left).
// First-come-first-serve among guild members.
func (k msgServer) ClaimGuildFounder(ctx context.Context, msg *types.MsgClaimGuildFounder) (*types.MsgClaimGuildFounderResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Get the guild
	guild, err := k.Guild.Get(ctx, msg.GuildId)
	if err != nil {
		return nil, errorsmod.Wrapf(types.ErrGuildNotFound, "guild %d not found", msg.GuildId)
	}

	// Check guild is frozen
	if guild.Status != types.GuildStatus_GUILD_STATUS_FROZEN {
		if guild.Status == types.GuildStatus_GUILD_STATUS_DISSOLVED {
			return nil, types.ErrGuildDissolved
		}
		return nil, types.ErrGuildNotFrozen
	}

	// Verify creator is a member of the guild
	if !k.IsGuildMember(ctx, msg.GuildId, msg.Creator) {
		return nil, types.ErrNotGuildMember
	}

	// A guild frozen below its minimum size keeps its founder; there is
	// nothing for that founder to claim.
	if k.canActAs(ctx, msg.Creator, guild.Founder) {
		return nil, types.ErrAlreadyGuildFounder
	}

	// Claim founder status
	oldFounder := guild.Founder
	guild.Founder = msg.Creator

	// If the new founder was an officer, remove from officers
	newOfficers := make([]string, 0, len(guild.Officers))
	for _, officer := range guild.Officers {
		if !k.canActAs(ctx, msg.Creator, officer) {
			newOfficers = append(newOfficers, officer)
		}
	}
	guild.Officers = newOfficers

	// Check if guild should be unfrozen (has enough members)
	params, getErr := k.Params.Get(ctx)
	if getErr != nil && !errors.Is(getErr, collections.ErrNotFound) {
		return nil, getErr
	}
	memberCount := k.GetGuildMemberCount(ctx, msg.GuildId)
	if memberCount >= uint64(params.MinGuildMembers) {
		guild.Status = types.GuildStatus_GUILD_STATUS_ACTIVE

		sdkCtx.EventManager().EmitEvent(
			sdk.NewEvent(
				"guild_unfrozen",
				sdk.NewAttribute("guild_id", fmt.Sprintf("%d", msg.GuildId)),
				sdk.NewAttribute("reason", "founder_claimed"),
			),
		)
	}

	if err := k.Guild.Set(ctx, msg.GuildId, guild); err != nil {
		return nil, errorsmod.Wrap(err, "failed to update guild")
	}

	// Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"guild_founder_claimed",
			sdk.NewAttribute("guild_id", fmt.Sprintf("%d", msg.GuildId)),
			sdk.NewAttribute("old_founder", oldFounder),
			sdk.NewAttribute("new_founder", msg.Creator),
		),
	)

	return &types.MsgClaimGuildFounderResponse{}, nil
}

package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/season/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// DemoteOfficer demotes a guild officer back to regular member.
// Only the founder can demote officers.
func (k msgServer) DemoteOfficer(ctx context.Context, msg *types.MsgDemoteOfficer) (*types.MsgDemoteOfficerResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}
	if _, err := k.addressCodec.StringToBytes(msg.Officer); err != nil {
		return nil, errorsmod.Wrap(err, "invalid officer address")
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

	// Verify creator is the founder
	if guild.Founder != msg.Creator {
		return nil, types.ErrNotGuildFounder
	}

	// Check if the member is actually an officer
	if !k.IsGuildOfficer(ctx, msg.GuildId, msg.Officer) {
		return nil, types.ErrNotOfficer
	}

	// Remove from officers
	newOfficers := make([]string, 0, len(guild.Officers)-1)
	for _, officer := range guild.Officers {
		if officer != msg.Officer {
			newOfficers = append(newOfficers, officer)
		}
	}
	guild.Officers = newOfficers

	if err := k.Guild.Set(ctx, msg.GuildId, guild); err != nil {
		return nil, errorsmod.Wrap(err, "failed to update guild")
	}

	// Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"guild_officer_demoted",
			sdk.NewAttribute("guild_id", fmt.Sprintf("%d", msg.GuildId)),
			sdk.NewAttribute("officer", msg.Officer),
			sdk.NewAttribute("demoted_by", msg.Creator),
		),
	)

	return &types.MsgDemoteOfficerResponse{}, nil
}

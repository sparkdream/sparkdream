package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/season/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// TransferGuildFounder transfers guild founder status to another member.
// Only the current founder can transfer ownership.
func (k msgServer) TransferGuildFounder(ctx context.Context, msg *types.MsgTransferGuildFounder) (*types.MsgTransferGuildFounderResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}
	if _, err := k.addressCodec.StringToBytes(msg.NewFounder); err != nil {
		return nil, errorsmod.Wrap(err, "invalid new founder address")
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

	// Verify the creator is the current founder
	if guild.Founder != msg.Creator {
		return nil, types.ErrNotGuildFounder
	}

	// Verify the new founder is a member of the guild
	if !k.IsGuildMember(ctx, msg.GuildId, msg.NewFounder) {
		return nil, types.ErrNotGuildMember
	}

	// Cannot transfer to self
	if msg.Creator == msg.NewFounder {
		return nil, errorsmod.Wrap(types.ErrNotGuildMember, "cannot transfer to self")
	}

	// If new founder was an officer, remove from officers list
	newOfficers := make([]string, 0, len(guild.Officers))
	for _, officer := range guild.Officers {
		if officer != msg.NewFounder {
			newOfficers = append(newOfficers, officer)
		}
	}
	guild.Officers = newOfficers

	// Transfer founder status
	oldFounder := guild.Founder
	guild.Founder = msg.NewFounder

	// If guild was frozen due to founder leaving, unfreeze it
	if guild.Status == types.GuildStatus_GUILD_STATUS_FROZEN {
		params, _ := k.Params.Get(ctx)
		memberCount := k.GetGuildMemberCount(ctx, msg.GuildId)
		if memberCount >= uint64(params.MinGuildMembers) {
			guild.Status = types.GuildStatus_GUILD_STATUS_ACTIVE
		}
	}

	if err := k.Guild.Set(ctx, msg.GuildId, guild); err != nil {
		return nil, errorsmod.Wrap(err, "failed to update guild")
	}

	// Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"guild_founder_transferred",
			sdk.NewAttribute("guild_id", fmt.Sprintf("%d", msg.GuildId)),
			sdk.NewAttribute("old_founder", oldFounder),
			sdk.NewAttribute("new_founder", msg.NewFounder),
		),
	)

	return &types.MsgTransferGuildFounderResponse{}, nil
}

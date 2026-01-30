package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/season/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// PromoteToOfficer promotes a guild member to officer status.
// Only the founder can promote officers.
func (k msgServer) PromoteToOfficer(ctx context.Context, msg *types.MsgPromoteToOfficer) (*types.MsgPromoteToOfficerResponse, error) {
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
	if guild.Status != types.GuildStatus_GUILD_STATUS_ACTIVE {
		if guild.Status == types.GuildStatus_GUILD_STATUS_DISSOLVED {
			return nil, types.ErrGuildDissolved
		}
		return nil, types.ErrGuildFrozen
	}

	// Verify creator is the founder
	if guild.Founder != msg.Creator {
		return nil, types.ErrNotGuildFounder
	}

	// Cannot promote self (founder)
	if msg.Member == msg.Creator {
		return nil, types.ErrFounderCannotBeOfficer
	}

	// Verify member is in the guild
	if !k.IsGuildMember(ctx, msg.GuildId, msg.Member) {
		return nil, types.ErrNotGuildMember
	}

	// Check if already an officer
	if k.IsGuildOfficer(ctx, msg.GuildId, msg.Member) {
		return nil, types.ErrAlreadyOfficer
	}

	// Check max officers
	params, _ := k.Params.Get(ctx)
	if uint32(len(guild.Officers)) >= params.MaxGuildOfficers {
		return nil, types.ErrMaxOfficers
	}

	// Add to officers
	guild.Officers = append(guild.Officers, msg.Member)

	if err := k.Guild.Set(ctx, msg.GuildId, guild); err != nil {
		return nil, errorsmod.Wrap(err, "failed to update guild")
	}

	// Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"guild_officer_promoted",
			sdk.NewAttribute("guild_id", fmt.Sprintf("%d", msg.GuildId)),
			sdk.NewAttribute("member", msg.Member),
			sdk.NewAttribute("promoted_by", msg.Creator),
		),
	)

	return &types.MsgPromoteToOfficerResponse{}, nil
}

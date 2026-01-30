package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/season/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// SetGuildInviteOnly toggles invite-only mode for a guild.
// Only the founder can change this setting.
func (k msgServer) SetGuildInviteOnly(ctx context.Context, msg *types.MsgSetGuildInviteOnly) (*types.MsgSetGuildInviteOnlyResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
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

	// Update invite only status
	guild.InviteOnly = msg.InviteOnly

	if err := k.Guild.Set(ctx, msg.GuildId, guild); err != nil {
		return nil, errorsmod.Wrap(err, "failed to update guild")
	}

	// Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"guild_invite_only_changed",
			sdk.NewAttribute("guild_id", fmt.Sprintf("%d", msg.GuildId)),
			sdk.NewAttribute("invite_only", fmt.Sprintf("%t", msg.InviteOnly)),
			sdk.NewAttribute("changed_by", msg.Creator),
		),
	)

	return &types.MsgSetGuildInviteOnlyResponse{}, nil
}

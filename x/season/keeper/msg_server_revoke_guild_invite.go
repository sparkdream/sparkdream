package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/season/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// RevokeGuildInvite revokes a pending guild invite.
// Founder or officers can revoke invites, or the invitee can decline.
func (k msgServer) RevokeGuildInvite(ctx context.Context, msg *types.MsgRevokeGuildInvite) (*types.MsgRevokeGuildInviteResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}
	if _, err := k.addressCodec.StringToBytes(msg.Invitee); err != nil {
		return nil, errorsmod.Wrap(err, "invalid invitee address")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Get the invite
	key := fmt.Sprintf("%d:%s", msg.GuildId, msg.Invitee)
	invite, err := k.GuildInvite.Get(ctx, key)
	if err != nil {
		return nil, types.ErrNoGuildInvite
	}

	// Get the guild
	guild, err := k.Guild.Get(ctx, msg.GuildId)
	if err != nil {
		return nil, errorsmod.Wrapf(types.ErrGuildNotFound, "guild %d not found", msg.GuildId)
	}

	// Check authorization: must be founder, officer, or the invitee themselves
	isFounderOrOfficer := k.IsGuildFounderOrOfficer(ctx, msg.GuildId, msg.Creator)
	isInvitee := msg.Creator == msg.Invitee

	if !isFounderOrOfficer && !isInvitee {
		return nil, types.ErrNotGuildFounderOrOfficer
	}

	// Remove the invite
	_ = k.GuildInvite.Remove(ctx, key)

	// Remove from pending invites list
	newInvites := make([]string, 0, len(guild.PendingInvites)-1)
	for _, inv := range guild.PendingInvites {
		if inv != msg.Invitee {
			newInvites = append(newInvites, inv)
		}
	}
	guild.PendingInvites = newInvites
	if err := k.Guild.Set(ctx, msg.GuildId, guild); err != nil {
		return nil, errorsmod.Wrap(err, "failed to update guild")
	}

	// Emit event
	revokeType := "revoked"
	if isInvitee {
		revokeType = "declined"
	}

	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"guild_invite_revoked",
			sdk.NewAttribute("guild_id", fmt.Sprintf("%d", msg.GuildId)),
			sdk.NewAttribute("invitee", msg.Invitee),
			sdk.NewAttribute("inviter", invite.Inviter),
			sdk.NewAttribute("revoked_by", msg.Creator),
			sdk.NewAttribute("type", revokeType),
		),
	)

	return &types.MsgRevokeGuildInviteResponse{}, nil
}

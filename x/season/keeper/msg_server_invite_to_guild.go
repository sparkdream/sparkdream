package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/season/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// InviteToGuild invites a member to join the guild.
// Founder or officers can invite.
func (k msgServer) InviteToGuild(ctx context.Context, msg *types.MsgInviteToGuild) (*types.MsgInviteToGuildResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}
	if _, err := k.addressCodec.StringToBytes(msg.Invitee); err != nil {
		return nil, errorsmod.Wrap(err, "invalid invitee address")
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

	// Verify creator is founder or officer
	if !k.IsGuildFounderOrOfficer(ctx, msg.GuildId, msg.Creator) {
		return nil, types.ErrNotGuildFounderOrOfficer
	}

	// Check invitee has a profile
	profile, err := k.MemberProfile.Get(ctx, msg.Invitee)
	if err != nil {
		return nil, errorsmod.Wrap(types.ErrProfileNotFound, "invitee profile not found")
	}

	// Check invitee is not already in a guild
	if profile.GuildId != 0 {
		return nil, errorsmod.Wrap(types.ErrAlreadyInGuild, "invitee is already in a guild")
	}

	// Check invitee doesn't already have an invite from this guild
	if k.HasPendingGuildInvite(ctx, msg.GuildId, msg.Invitee) {
		return nil, types.ErrAlreadyInvited
	}

	// Check max pending invites
	params, _ := k.Params.Get(ctx)
	if uint32(len(guild.PendingInvites)) >= params.MaxPendingInvites {
		return nil, types.ErrMaxPendingInvites
	}

	// Create the invite
	currentEpoch := k.GetCurrentEpoch(ctx)
	expiresEpoch := int64(0)
	if params.GuildInviteTtlEpochs > 0 {
		expiresEpoch = currentEpoch + int64(params.GuildInviteTtlEpochs)
	}

	invite := types.GuildInvite{
		GuildInvitee: msg.Invitee,
		Inviter:      msg.Creator,
		CreatedEpoch: currentEpoch,
		ExpiresEpoch: expiresEpoch,
	}

	key := fmt.Sprintf("%d:%s", msg.GuildId, msg.Invitee)
	if err := k.GuildInvite.Set(ctx, key, invite); err != nil {
		return nil, errorsmod.Wrap(err, "failed to save invite")
	}

	// Add to pending invites list
	guild.PendingInvites = append(guild.PendingInvites, msg.Invitee)
	if err := k.Guild.Set(ctx, msg.GuildId, guild); err != nil {
		return nil, errorsmod.Wrap(err, "failed to update guild")
	}

	// Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"guild_invite_created",
			sdk.NewAttribute("guild_id", fmt.Sprintf("%d", msg.GuildId)),
			sdk.NewAttribute("invitee", msg.Invitee),
			sdk.NewAttribute("inviter", msg.Creator),
			sdk.NewAttribute("expires_epoch", fmt.Sprintf("%d", expiresEpoch)),
		),
	)

	return &types.MsgInviteToGuildResponse{}, nil
}

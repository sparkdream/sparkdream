package keeper

import (
	"context"
	"errors"
	"fmt"

	"cosmossdk.io/collections"

	commontypes "sparkdream/x/common/types"
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

	// Free the slots of expired invites first, so an expired invite neither
	// blocks a re-invite nor counts against max_pending_invites (SEA-11).
	if err := k.pruneExpiredGuildInvitesOf(ctx, msg.GuildId, k.GetCurrentEpoch(ctx)); err != nil {
		return nil, errorsmod.Wrap(err, "failed to expire stale invites")
	}
	if guild, err = k.Guild.Get(ctx, msg.GuildId); err != nil {
		return nil, errorsmod.Wrapf(types.ErrGuildNotFound, "guild %d not found", msg.GuildId)
	}

	// Check invitee doesn't already have an invite from this guild
	if k.HasPendingGuildInvite(ctx, msg.GuildId, msg.Invitee) {
		return nil, types.ErrAlreadyInvited
	}

	// Check max pending invites
	params, getErr := k.Params.Get(ctx)
	if getErr != nil && !errors.Is(getErr, collections.ErrNotFound) {
		return nil, getErr
	}
	if commontypes.Saturate[uint32](len(guild.PendingInvites)) >= params.MaxPendingInvites {
		return nil, types.ErrMaxPendingInvites
	}

	// Create the invite
	currentEpoch := k.GetCurrentEpoch(ctx)
	expiresEpoch := int64(0)
	if params.GuildInviteTtlEpochs > 0 {
		expiresEpoch = currentEpoch + commontypes.Saturate[int64](params.GuildInviteTtlEpochs)
	}

	invite := types.GuildInvite{
		GuildInvitee: msg.Invitee,
		Inviter:      msg.Creator,
		CreatedEpoch: currentEpoch,
		ExpiresEpoch: expiresEpoch,
		GuildId:      msg.GuildId,
	}

	key := guildInviteKey(msg.GuildId, msg.Invitee)
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

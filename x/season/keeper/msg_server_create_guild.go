package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/season/types"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// CreateGuild creates a new guild with the creator as founder.
// Guild creation costs DREAM and the founder becomes the first member.
func (k msgServer) CreateGuild(ctx context.Context, msg *types.MsgCreateGuild) (*types.MsgCreateGuildResponse, error) {
	if _, err := k.addressCodec.StringToBytes(msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "invalid creator address")
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Check maintenance mode
	if k.IsInMaintenanceMode(ctx) {
		return nil, types.ErrMaintenanceMode
	}

	// Validate guild name
	if err := k.ValidateGuildName(ctx, msg.Name); err != nil {
		return nil, err
	}

	// Validate guild description
	if err := k.ValidateGuildDescription(ctx, msg.Description); err != nil {
		return nil, err
	}

	// Get member profile (must exist and not be in a guild)
	profile, err := k.MemberProfile.Get(ctx, msg.Creator)
	if err != nil {
		return nil, errorsmod.Wrap(types.ErrProfileNotFound, "member profile not found")
	}

	if profile.GuildId != 0 {
		return nil, types.ErrAlreadyInGuild
	}

	// Check guild hop cooldown
	membership, err := k.GuildMembership.Get(ctx, msg.Creator)
	if err == nil && membership.LeftEpoch > 0 {
		params, _ := k.Params.Get(ctx)
		currentEpoch := k.GetCurrentEpoch(ctx)
		epochsSinceLeft := currentEpoch - membership.LeftEpoch
		if uint64(epochsSinceLeft) < params.GuildHopCooldownEpochs {
			return nil, errorsmod.Wrapf(types.ErrGuildHopCooldown,
				"must wait %d more epochs", params.GuildHopCooldownEpochs-uint64(epochsSinceLeft))
		}
	}

	// Check max guilds per season
	params, _ := k.Params.Get(ctx)
	if err == nil && membership.GuildsJoinedThisSeason >= uint64(params.MaxGuildsPerSeason) {
		return nil, types.ErrMaxGuildsPerSeason
	}

	// Burn DREAM cost via x/rep integration
	if err := k.BurnDREAM(ctx, msg.Creator, params.GuildCreationCost.Uint64()); err != nil {
		return nil, errorsmod.Wrap(types.ErrDREAMOperationFailed, "failed to burn DREAM for guild creation")
	}

	// Reserve guild name via x/name integration
	if err := k.ReserveName(ctx, msg.Name, "guild", msg.Creator); err != nil {
		return nil, errorsmod.Wrap(err, "failed to reserve guild name")
	}

	// Get next guild ID (add 1 because 0 means "no guild")
	seqVal, err := k.GuildSeq.Next(ctx)
	if err != nil {
		return nil, errorsmod.Wrap(err, "failed to get next guild ID")
	}
	guildID := seqVal + 1

	// Create the guild
	guild := types.Guild{
		Id:             guildID,
		Name:           msg.Name,
		Description:    msg.Description,
		Founder:        msg.Creator,
		CreatedBlock:   sdkCtx.BlockHeight(),
		InviteOnly:     msg.InviteOnly,
		Status:         types.GuildStatus_GUILD_STATUS_ACTIVE,
		Officers:       []string{},
		PendingInvites: []string{},
	}

	if err := k.Guild.Set(ctx, guildID, guild); err != nil {
		return nil, errorsmod.Wrap(err, "failed to save guild")
	}

	// Update member profile
	profile.GuildId = guildID
	profile.LastActiveEpoch = k.GetCurrentEpoch(ctx)
	if err := k.MemberProfile.Set(ctx, msg.Creator, profile); err != nil {
		return nil, errorsmod.Wrap(err, "failed to update profile")
	}

	// Create/update membership record
	currentEpoch := k.GetCurrentEpoch(ctx)
	newMembership := types.GuildMembership{
		Member:                  msg.Creator,
		GuildId:                 guildID,
		JoinedEpoch:             currentEpoch,
		LeftEpoch:               0,
		GuildsJoinedThisSeason:  1,
	}
	if err == nil {
		newMembership.GuildsJoinedThisSeason = membership.GuildsJoinedThisSeason + 1
	}
	if err := k.GuildMembership.Set(ctx, msg.Creator, newMembership); err != nil {
		return nil, errorsmod.Wrap(err, "failed to save membership")
	}

	// Emit event
	sdkCtx.EventManager().EmitEvent(
		sdk.NewEvent(
			"guild_created",
			sdk.NewAttribute("guild_id", fmt.Sprintf("%d", guildID)),
			sdk.NewAttribute("name", msg.Name),
			sdk.NewAttribute("founder", msg.Creator),
			sdk.NewAttribute("invite_only", fmt.Sprintf("%t", msg.InviteOnly)),
		),
	)

	return &types.MsgCreateGuildResponse{}, nil
}

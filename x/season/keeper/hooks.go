package keeper

import (
	"context"
	"fmt"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/season/types"
)

// ---------------------------------------------------------------------------
// Hook entry points (x-season-spec "Hook Implementations"). x/rep, x/commons
// and x/forum call these after their own state writes; every one is
// best-effort for the caller (log and continue), so nothing here may be
// tx-halting for the originating action. XP cap errors are swallowed.
// ---------------------------------------------------------------------------

func swallowCap(err error) error {
	if err != nil && errorsmod.IsOf(err, types.ErrXPCapReached) {
		return nil
	}
	return err
}

// --- x/rep -------------------------------------------------------------------

// OnMemberCreated opens the member's profile and records the registration
// epoch the forum account-age check reads.
func (k Keeper) OnMemberCreated(ctx context.Context, member sdk.AccAddress) error {
	memberStr, err := k.addressCodec.BytesToString(member)
	if err != nil {
		return err
	}
	env, err := k.loadXPEnv(ctx)
	if err != nil {
		return err
	}
	epoch := env.epoch
	if !k.HasMemberProfile(ctx, memberStr) {
		if err := k.MemberProfile.Set(ctx, memberStr, types.MemberProfile{
			Address: memberStr, SeasonLevel: 1, LastActiveEpoch: epoch,
		}); err != nil {
			return err
		}
	}
	if _, err := k.MemberRegistration.Get(ctx, memberStr); err != nil {
		if err := k.MemberRegistration.Set(ctx, memberStr, types.MemberRegistration{
			Member: memberStr, RegisteredEpoch: epoch,
		}); err != nil {
			return err
		}
	}
	season, _ := k.Season.Get(ctx)
	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent("member_profile_created",
		sdk.NewAttribute("member", memberStr),
		sdk.NewAttribute("season", fmt.Sprintf("%d", season.Number)),
	))
	return nil
}

// inviterOf returns the member's inviter, if any.
func (k Keeper) inviterOf(ctx context.Context, member sdk.AccAddress) (sdk.AccAddress, string, bool) {
	if k.repKeeper == nil {
		return nil, "", false
	}
	m, err := k.repKeeper.GetMember(ctx, member)
	if err != nil || m.InvitedBy == "" {
		return nil, "", false
	}
	inviter, err := k.addressCodec.StringToBytes(m.InvitedBy)
	if err != nil {
		return nil, "", false
	}
	return inviter, m.InvitedBy, true
}

// OnMemberEstablished pays the inviter the mentorship XP and bumps their
// successful-invitation counter.
func (k Keeper) OnMemberEstablished(ctx context.Context, member sdk.AccAddress) error {
	inviter, inviterStr, ok := k.inviterOf(ctx, member)
	if !ok {
		return nil
	}
	env, err := k.loadXPEnv(ctx)
	if err != nil {
		return err
	}
	memberStr, _ := k.addressCodec.BytesToString(member)
	if err := swallowCap(k.grantXP(ctx, env, inviter, env.params.XpInviteeEstablished, types.XPSourceInviteeEstablished, memberStr)); err != nil {
		return err
	}
	if err := k.profileCounter(ctx, inviterStr, env.epoch, func(p *types.MemberProfile) { p.InvitationsSuccessful++ }); err != nil {
		return err
	}
	if err := k.OnQuestObjectiveProgress(ctx, inviter, types.QuestObjectiveType_QUEST_OBJECTIVE_TYPE_INVITEE_MILESTONE, 1); err != nil {
		return err
	}
	return k.checkAchievements(ctx, env, inviter, []types.RequirementType{
		types.RequirementType_REQUIREMENT_TYPE_INVITATIONS_SUCCESSFUL, types.RequirementType_REQUIREMENT_TYPE_MIN_LEVEL,
	})
}

// OnInitiativeCompleted advances the member's initiative objectives and
// achievements; on their first completion it also pays the inviter.
func (k Keeper) OnInitiativeCompleted(ctx context.Context, member sdk.AccAddress, initiativeID uint64) error {
	env, err := k.loadXPEnv(ctx)
	if err != nil {
		return err
	}
	if err := k.OnQuestObjectiveProgress(ctx, member, types.QuestObjectiveType_QUEST_OBJECTIVE_TYPE_INITIATIVES_COMPLETED, 1); err != nil {
		return err
	}
	if err := k.checkAchievements(ctx, env, member, []types.RequirementType{
		types.RequirementType_REQUIREMENT_TYPE_INITIATIVES_COMPLETED, types.RequirementType_REQUIREMENT_TYPE_REPUTATION_EARNED,
	}); err != nil {
		return err
	}
	if k.repKeeper == nil {
		return nil
	}
	memberStr, _ := k.addressCodec.BytesToString(member)
	count, err := k.repKeeper.GetCompletedInitiativesCount(ctx, memberStr)
	if err != nil || count != 1 {
		return nil
	}
	inviter, _, ok := k.inviterOf(ctx, member)
	if !ok {
		return nil
	}
	if err := swallowCap(k.grantXP(ctx, env, inviter, env.params.XpInviteeFirstInitiative, types.XPSourceInviteeFirstInitiative, fmt.Sprintf("%s:initiative:%d", memberStr, initiativeID))); err != nil {
		return err
	}
	return k.OnQuestObjectiveProgress(ctx, inviter, types.QuestObjectiveType_QUEST_OBJECTIVE_TYPE_INVITEE_MILESTONE, 1)
}

// OnChallengeResolved counts a won challenge toward the challenger's
// achievements. A lost challenge moves nothing.
func (k Keeper) OnChallengeResolved(ctx context.Context, challenger, challengee sdk.AccAddress, challengerWon bool) error {
	if !challengerWon {
		return nil
	}
	challengerStr, err := k.addressCodec.BytesToString(challenger)
	if err != nil {
		return err
	}
	env, err := k.loadXPEnv(ctx)
	if err != nil {
		return err
	}
	if err := k.profileCounter(ctx, challengerStr, env.epoch, func(p *types.MemberProfile) { p.ChallengesWon++ }); err != nil {
		return err
	}
	return k.checkAchievements(ctx, env, challenger, []types.RequirementType{types.RequirementType_REQUIREMENT_TYPE_CHALLENGES_WON})
}

// OnJuryDutyCompleted counts a served jury seat toward the juror's
// achievements.
func (k Keeper) OnJuryDutyCompleted(ctx context.Context, juror sdk.AccAddress, juryReviewID uint64) error {
	jurorStr, err := k.addressCodec.BytesToString(juror)
	if err != nil {
		return err
	}
	env, err := k.loadXPEnv(ctx)
	if err != nil {
		return err
	}
	if err := k.profileCounter(ctx, jurorStr, env.epoch, func(p *types.MemberProfile) { p.JuryDutiesCompleted++ }); err != nil {
		return err
	}
	return k.checkAchievements(ctx, env, juror, []types.RequirementType{types.RequirementType_REQUIREMENT_TYPE_JURY_DUTY})
}

// OnMemberZeroed removes the member from their guild, resets season XP and
// achievement progress counters and drops in-progress quests. Earned
// achievements, titles and lifetime XP are kept: "punish position, not
// person".
func (k Keeper) OnMemberZeroed(ctx context.Context, member sdk.AccAddress) error {
	memberStr, err := k.addressCodec.BytesToString(member)
	if err != nil {
		return err
	}
	if err := k.clearMemberQuestProgress(ctx, memberStr); err != nil {
		return err
	}
	profile, err := k.MemberProfile.Get(ctx, memberStr)
	if err != nil {
		return nil
	}
	guildRemoved := uint64(0)
	if profile.GuildId != 0 {
		guildRemoved = profile.GuildId
		if err := k.removeFromGuildOnZeroing(ctx, memberStr, &profile); err != nil {
			return err
		}
	}
	profile.SeasonXp = 0
	profile.SeasonLevel = 1
	profile.ChallengesWon = 0
	profile.JuryDutiesCompleted = 0
	profile.VotesCast = 0
	profile.ForumHelpfulCount = 0
	profile.InvitationsSuccessful = 0
	if err := k.MemberProfile.Set(ctx, memberStr, profile); err != nil {
		return err
	}
	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent("member_zeroed_cleanup",
		sdk.NewAttribute("member", memberStr),
		sdk.NewAttribute("guild_removed", fmt.Sprintf("%d", guildRemoved)),
		sdk.NewAttribute("achievement_progress_reset", "true"),
	))
	return nil
}

// removeFromGuildOnZeroing takes a zeroed member out of their guild. A plain
// member or officer is removed like LeaveGuild would; a founder cannot leave
// through MsgLeaveGuild, so the guild is frozen instead and any remaining
// member may take it over with MsgClaimGuildFounder. The profile's guild
// field is cleared in place; the caller persists the profile.
func (k Keeper) removeFromGuildOnZeroing(ctx context.Context, memberStr string, profile *types.MemberProfile) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	guildID := profile.GuildId
	epoch := k.GetCurrentEpoch(ctx)
	profile.GuildId = 0

	membership, _ := k.GuildMembership.Get(ctx, memberStr)
	membership.GuildId = 0
	membership.LeftEpoch = epoch
	if err := k.GuildMembership.Set(ctx, memberStr, membership); err != nil {
		return err
	}
	k.DecrementGuildMemberCount(ctx, guildID)

	guild, err := k.Guild.Get(ctx, guildID)
	if err != nil {
		return nil
	}
	officers := make([]string, 0, len(guild.Officers))
	for _, o := range guild.Officers {
		if o != memberStr {
			officers = append(officers, o)
		}
	}
	guild.Officers = officers

	params, _ := k.Params.Get(ctx)
	reason := ""
	switch {
	case guild.Founder == memberStr && guild.Status == types.GuildStatus_GUILD_STATUS_ACTIVE:
		reason = "founder_zeroed"
	case k.GetGuildMemberCount(ctx, guildID) < uint64(params.MinGuildMembers) && guild.Status == types.GuildStatus_GUILD_STATUS_ACTIVE:
		reason = "below_minimum_members"
	}
	if reason != "" {
		guild.Status = types.GuildStatus_GUILD_STATUS_FROZEN
		sdkCtx.EventManager().EmitEvent(sdk.NewEvent("guild_frozen",
			sdk.NewAttribute("guild_id", fmt.Sprintf("%d", guildID)),
			sdk.NewAttribute("reason", reason),
		))
	}
	if err := k.Guild.Set(ctx, guildID, guild); err != nil {
		return err
	}
	sdkCtx.EventManager().EmitEvent(sdk.NewEvent("guild_left",
		sdk.NewAttribute("guild_id", fmt.Sprintf("%d", guildID)),
		sdk.NewAttribute("member", memberStr),
		sdk.NewAttribute("reason", "zeroed"),
	))
	return nil
}

// --- x/commons (governance) ------------------------------------------------

// OnVoteCast grants vote XP once per (season, voter, proposal), at most
// max_vote_xp_per_epoch times per epoch, and advances vote objectives.
func (k Keeper) OnVoteCast(ctx context.Context, voter sdk.AccAddress, proposalID uint64) error {
	voterStr, err := k.addressCodec.BytesToString(voter)
	if err != nil {
		return err
	}
	env, err := k.loadXPEnv(ctx)
	if err != nil {
		return err
	}
	params, epoch := env.params, env.epoch
	season, err := k.Season.Get(ctx)
	if err != nil {
		return err
	}
	if k.hasVoteXPRecord(ctx, season.Number, voterStr, proposalID) {
		return nil
	}
	if params.MaxVoteXpPerEpoch > 0 && k.voteXPCountThisEpoch(ctx, voterStr, epoch, params.XpVoteCast) >= uint64(params.MaxVoteXpPerEpoch) {
		return nil
	}
	// One profile load, one write: XP, counter and any achievement the
	// vote unlocks are applied in memory first.
	profile := k.getOrCreateProfile(ctx, voterStr, epoch)
	if err := swallowCap(k.grantXPToProfile(ctx, env, &profile, params.XpVoteCast, types.XPSourceVoteCast, fmt.Sprintf("proposal:%d", proposalID))); err != nil {
		return err
	}
	key := voteXPRecordKey(season.Number, voterStr, proposalID)
	if err := k.VoteXpRecord.Set(ctx, key, types.VoteXpRecord{
		SeasonMemberProposal: key, GrantedBlock: sdk.UnwrapSDKContext(ctx).BlockHeight(),
	}); err != nil {
		return err
	}
	profile.VotesCast++
	profile.LastActiveEpoch = epoch
	if _, err := k.checkAchievementsOnProfile(ctx, env, &profile, []types.RequirementType{
		types.RequirementType_REQUIREMENT_TYPE_VOTES_CAST, types.RequirementType_REQUIREMENT_TYPE_MIN_LEVEL,
	}); err != nil {
		return err
	}
	if err := k.MemberProfile.Set(ctx, voterStr, profile); err != nil {
		return err
	}
	return k.OnQuestObjectiveProgress(ctx, voter, types.QuestObjectiveType_QUEST_OBJECTIVE_TYPE_VOTES_CAST, 1)
}

// OnProposalCreated grants the proposer the proposal XP.
func (k Keeper) OnProposalCreated(ctx context.Context, proposer sdk.AccAddress, proposalID uint64) error {
	env, err := k.loadXPEnv(ctx)
	if err != nil {
		return err
	}
	return swallowCap(k.grantXP(ctx, env, proposer, env.params.XpProposalCreated, types.XPSourceProposalCreated, fmt.Sprintf("proposal:%d", proposalID)))
}

// --- x/forum -----------------------------------------------------------------

// OnForumReplyReceived grants the parent post's author reply XP under the
// forum anti-gaming rules.
func (k Keeper) OnForumReplyReceived(ctx context.Context, postAuthor, replier string, postID uint64) error {
	env, err := k.loadXPEnv(ctx)
	if err != nil {
		return err
	}
	_, err = k.grantForumXP(ctx, env, postAuthor, replier, env.params.XpForumReplyReceived, types.XPSourceForumReply,
		fmt.Sprintf("post:%d:replier:%s", postID, replier))
	return err
}

// OnForumPostMarkedHelpful grants the author helpful XP under the forum
// anti-gaming rules, counts it toward achievements and advances
// FORUM_HELPFUL objectives.
func (k Keeper) OnForumPostMarkedHelpful(ctx context.Context, postAuthor, marker string, postID uint64, isReply bool) error {
	env, err := k.loadXPEnv(ctx)
	if err != nil {
		return err
	}
	granted, err := k.grantForumXP(ctx, env, postAuthor, marker, env.params.XpForumMarkedHelpful, types.XPSourceForumHelpful,
		fmt.Sprintf("post:%d:marker:%s", postID, marker))
	if err != nil || !granted {
		return err
	}
	if err := k.profileCounter(ctx, postAuthor, env.epoch, func(p *types.MemberProfile) { p.ForumHelpfulCount++ }); err != nil {
		return err
	}
	author, err := k.addressCodec.StringToBytes(postAuthor)
	if err != nil {
		return nil
	}
	if err := k.OnQuestObjectiveProgress(ctx, author, types.QuestObjectiveType_QUEST_OBJECTIVE_TYPE_FORUM_HELPFUL, 1); err != nil {
		return err
	}
	return k.checkAchievements(ctx, env, author, []types.RequirementType{
		types.RequirementType_REQUIREMENT_TYPE_FORUM_HELPFUL, types.RequirementType_REQUIREMENT_TYPE_MIN_LEVEL,
	})
}

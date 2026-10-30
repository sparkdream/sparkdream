package keeper

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"sparkdream/x/season/types"

	"cosmossdk.io/collections"
)

// InitGenesis initializes the module's state from a provided genesis state.
func (k Keeper) InitGenesis(ctx context.Context, genState types.GenesisState) error {
	if genState.Season != nil {
		if err := k.Season.Set(ctx, *genState.Season); err != nil {
			return err
		}
	}
	if genState.SeasonTransitionState != nil {
		if err := k.SeasonTransitionState.Set(ctx, *genState.SeasonTransitionState); err != nil {
			return err
		}
	}
	if genState.TransitionRecoveryState != nil {
		if err := k.TransitionRecoveryState.Set(ctx, *genState.TransitionRecoveryState); err != nil {
			return err
		}
	}
	if genState.NextSeasonInfo != nil {
		if err := k.NextSeasonInfo.Set(ctx, *genState.NextSeasonInfo); err != nil {
			return err
		}
	}

	// Bootstrap sample titles and achievements only if none provided in genesis
	// This allows custom genesis states to fully control achievements/titles
	if len(genState.AchievementMap) == 0 && len(genState.TitleMap) == 0 {
		k.BootstrapTitlesAndAchievements(ctx)
	}

	for _, elem := range genState.SeasonSnapshotMap {
		if err := k.SeasonSnapshot.Set(ctx, elem.Season, elem); err != nil {
			return err
		}
	}
	for _, elem := range genState.MemberSeasonSnapshotMap {
		if err := k.MemberSeasonSnapshot.Set(ctx, elem.SeasonAddress, elem); err != nil {
			return err
		}
	}
	for _, elem := range genState.MemberProfileMap {
		if err := k.MemberProfile.Set(ctx, elem.Address, elem); err != nil {
			return err
		}
		// UsernameOwner is derived state, rebuilt here (SEA-28).
		if elem.Username != "" {
			if err := k.UsernameOwner.Set(ctx, strings.ToLower(elem.Username), elem.Address); err != nil {
				return err
			}
		}
	}
	for _, elem := range genState.MemberRegistrationMap {
		if err := k.MemberRegistration.Set(ctx, elem.Member, elem); err != nil {
			return err
		}
	}
	for _, elem := range genState.AchievementMap {
		if err := k.SetAchievement(ctx, elem); err != nil {
			return err
		}
	}
	for _, elem := range genState.TitleMap {
		if err := k.SetTitle(ctx, elem); err != nil {
			return err
		}
	}
	for _, elem := range genState.SeasonTitleEligibilityMap {
		if err := k.SeasonTitleEligibility.Set(ctx, elem.TitleSeason, elem); err != nil {
			return err
		}
	}
	for _, elem := range genState.GuildList {
		if err := k.Guild.Set(ctx, elem.Id, elem); err != nil {
			return err
		}
	}

	if err := k.GuildSeq.Set(ctx, genState.GuildCount); err != nil {
		return err
	}
	activeMembers := map[uint64]uint64{}
	for _, elem := range genState.GuildMembershipMap {
		if err := k.GuildMembership.Set(ctx, elem.Member, elem); err != nil {
			return err
		}
		if g := activeGuildOf(elem); g != 0 {
			activeMembers[g]++
		}
	}
	// GuildMemberCount is derived state (SEA-11): rebuilt here so the first
	// lookup after an import is not a full membership scan.
	for guildID, n := range activeMembers {
		if err := k.GuildMemberCount.Set(ctx, guildID, n); err != nil {
			return err
		}
	}
	// Invites are keyed "<guild_id>:<invitee>" like every write path
	// (SEA-25).
	for _, elem := range genState.GuildInviteMap {
		if err := k.GuildInvite.Set(ctx, guildInviteKey(elem.GuildId, elem.GuildInvitee), elem); err != nil {
			return err
		}
	}
	for _, elem := range genState.QuestMap {
		if err := k.Quest.Set(ctx, elem.QuestId, elem); err != nil {
			return err
		}
	}
	for _, elem := range genState.MemberQuestProgressMap {
		if err := k.MemberQuestProgress.Set(ctx, elem.MemberQuest, elem); err != nil {
			return err
		}
	}
	for _, elem := range genState.EpochXpTrackerMap {
		if err := k.EpochXpTracker.Set(ctx, elem.MemberEpoch, elem); err != nil {
			return err
		}
	}
	for _, elem := range genState.VoteXpRecordMap {
		if err := k.VoteXpRecord.Set(ctx, elem.SeasonMemberProposal, elem); err != nil {
			return err
		}
	}
	for _, elem := range genState.ForumXpCooldownMap {
		if err := k.ForumXpCooldown.Set(ctx, elem.BeneficiaryActor, elem); err != nil {
			return err
		}
	}
	for _, elem := range genState.DisplayNameModerationMap {
		if err := k.SetDisplayNameModeration(ctx, elem.Member, elem); err != nil {
			return err
		}
	}
	for _, elem := range genState.DisplayNameReportStakeMap {
		if err := k.DisplayNameReportStake.Set(ctx, elem.ChallengeId, elem); err != nil {
			return err
		}
	}
	for _, elem := range genState.DisplayNameAppealStakeMap {
		if err := k.DisplayNameAppealStake.Set(ctx, elem.ChallengeId, elem); err != nil {
			return err
		}
	}

	for _, elem := range genState.NominationList {
		if err := k.SetNomination(ctx, elem); err != nil {
			return err
		}
		// The ref set and nominator counters are derived state (SEA-20).
		if err := k.indexNewNomination(ctx, elem); err != nil {
			return err
		}
	}
	if err := k.NominationSeq.Set(ctx, genState.NominationCount); err != nil {
		return err
	}
	for _, elem := range genState.NominationStakeList {
		key := fmt.Sprintf("%d/%s", elem.NominationId, elem.Staker)
		if err := k.NominationStake.Set(ctx, key, elem); err != nil {
			return err
		}
	}
	for _, elem := range genState.RetroRewardRecordList {
		key := fmt.Sprintf("%d/%d", elem.Season, elem.NominationId)
		if err := k.RetroRewardRecord.Set(ctx, key, elem); err != nil {
			return err
		}
	}

	for _, elem := range genState.TransitionAbortList {
		if err := k.TransitionAborts.Set(ctx, elem.Season, elem.Aborts); err != nil {
			return err
		}
	}
	// Running per-season aggregates (SEA-35): the current season's go to
	// the running item, earlier ones to the archive. The current season's
	// XP aggregates are derived from the imported profiles (a genesis may
	// seed season XP without counters); the event counts are imported.
	var current types.SeasonStatsCounters
	for _, elem := range genState.SeasonStatsCountersList {
		if genState.Season != nil && elem.Season == genState.Season.Number {
			current = elem
			continue
		}
		if err := k.SeasonStatsCounters.Set(ctx, elem.Season, elem); err != nil {
			return err
		}
	}
	if genState.Season != nil {
		current.Season = genState.Season.Number
		current.TotalXpEarned, current.ActiveMembers = 0, 0
		for _, p := range genState.MemberProfileMap {
			if p.SeasonXp > 0 {
				current.TotalXpEarned = satAdd(current.TotalXpEarned, p.SeasonXp)
				current.ActiveMembers++
			}
		}
		if err := k.CurrentSeasonStats.Set(ctx, current); err != nil {
			return err
		}
	}
	if genState.PendingSeasonalPoolInit != 0 {
		if err := k.PendingSeasonalPoolInit.Set(ctx, genState.PendingSeasonalPoolInit); err != nil {
			return err
		}
	}

	return k.Params.Set(ctx, genState.Params)
}

// ExportGenesis returns the module's exported genesis.
func (k Keeper) ExportGenesis(ctx context.Context) (*types.GenesisState, error) {
	var err error

	genesis := types.DefaultGenesis()
	genesis.Params, err = k.Params.Get(ctx)
	if err != nil {
		return nil, err
	}
	season, err := k.Season.Get(ctx)
	if err != nil && !errors.Is(err, collections.ErrNotFound) {
		return nil, err
	}
	genesis.Season = &season
	// Absent items stay nil so InitGenesis does not materialize them.
	if seasonTransitionState, err := k.SeasonTransitionState.Get(ctx); err == nil {
		genesis.SeasonTransitionState = &seasonTransitionState
	} else if !errors.Is(err, collections.ErrNotFound) {
		return nil, err
	}
	// Absent items stay nil so InitGenesis does not materialize them.
	if transitionRecoveryState, err := k.TransitionRecoveryState.Get(ctx); err == nil {
		genesis.TransitionRecoveryState = &transitionRecoveryState
	} else if !errors.Is(err, collections.ErrNotFound) {
		return nil, err
	}
	// Absent items stay nil so InitGenesis does not materialize them.
	if nextSeasonInfo, err := k.NextSeasonInfo.Get(ctx); err == nil {
		genesis.NextSeasonInfo = &nextSeasonInfo
	} else if !errors.Is(err, collections.ErrNotFound) {
		return nil, err
	}
	if err := k.SeasonSnapshot.Walk(ctx, nil, func(_ uint64, val types.SeasonSnapshot) (stop bool, err error) {
		genesis.SeasonSnapshotMap = append(genesis.SeasonSnapshotMap, val)
		return false, nil
	}); err != nil {
		return nil, err
	}
	if err := k.MemberSeasonSnapshot.Walk(ctx, nil, func(_ string, val types.MemberSeasonSnapshot) (stop bool, err error) {
		genesis.MemberSeasonSnapshotMap = append(genesis.MemberSeasonSnapshotMap, val)
		return false, nil
	}); err != nil {
		return nil, err
	}
	if err := k.MemberProfile.Walk(ctx, nil, func(_ string, val types.MemberProfile) (stop bool, err error) {
		genesis.MemberProfileMap = append(genesis.MemberProfileMap, val)
		return false, nil
	}); err != nil {
		return nil, err
	}
	if err := k.MemberRegistration.Walk(ctx, nil, func(_ string, val types.MemberRegistration) (stop bool, err error) {
		genesis.MemberRegistrationMap = append(genesis.MemberRegistrationMap, val)
		return false, nil
	}); err != nil {
		return nil, err
	}
	if err := k.Achievement.Walk(ctx, nil, func(_ string, val types.Achievement) (stop bool, err error) {
		genesis.AchievementMap = append(genesis.AchievementMap, val)
		return false, nil
	}); err != nil {
		return nil, err
	}
	if err := k.Title.Walk(ctx, nil, func(_ string, val types.Title) (stop bool, err error) {
		genesis.TitleMap = append(genesis.TitleMap, val)
		return false, nil
	}); err != nil {
		return nil, err
	}
	if err := k.SeasonTitleEligibility.Walk(ctx, nil, func(_ uint64, val types.SeasonTitleEligibility) (stop bool, err error) {
		genesis.SeasonTitleEligibilityMap = append(genesis.SeasonTitleEligibilityMap, val)
		return false, nil
	}); err != nil {
		return nil, err
	}
	err = k.Guild.Walk(ctx, nil, func(key uint64, elem types.Guild) (bool, error) {
		genesis.GuildList = append(genesis.GuildList, elem)
		return false, nil
	})
	if err != nil {
		return nil, err
	}

	genesis.GuildCount, err = k.GuildSeq.Peek(ctx)
	if err != nil {
		return nil, err
	}
	if err := k.GuildMembership.Walk(ctx, nil, func(_ string, val types.GuildMembership) (stop bool, err error) {
		genesis.GuildMembershipMap = append(genesis.GuildMembershipMap, val)
		return false, nil
	}); err != nil {
		return nil, err
	}
	if err := k.GuildInvite.Walk(ctx, nil, func(_ string, val types.GuildInvite) (stop bool, err error) {
		genesis.GuildInviteMap = append(genesis.GuildInviteMap, val)
		return false, nil
	}); err != nil {
		return nil, err
	}
	if err := k.Quest.Walk(ctx, nil, func(_ string, val types.Quest) (stop bool, err error) {
		genesis.QuestMap = append(genesis.QuestMap, val)
		return false, nil
	}); err != nil {
		return nil, err
	}
	if err := k.MemberQuestProgress.Walk(ctx, nil, func(_ string, val types.MemberQuestProgress) (stop bool, err error) {
		genesis.MemberQuestProgressMap = append(genesis.MemberQuestProgressMap, val)
		return false, nil
	}); err != nil {
		return nil, err
	}
	if err := k.EpochXpTracker.Walk(ctx, nil, func(_ string, val types.EpochXpTracker) (stop bool, err error) {
		genesis.EpochXpTrackerMap = append(genesis.EpochXpTrackerMap, val)
		return false, nil
	}); err != nil {
		return nil, err
	}
	if err := k.VoteXpRecord.Walk(ctx, nil, func(_ string, val types.VoteXpRecord) (stop bool, err error) {
		genesis.VoteXpRecordMap = append(genesis.VoteXpRecordMap, val)
		return false, nil
	}); err != nil {
		return nil, err
	}
	if err := k.ForumXpCooldown.Walk(ctx, nil, func(_ string, val types.ForumXpCooldown) (stop bool, err error) {
		genesis.ForumXpCooldownMap = append(genesis.ForumXpCooldownMap, val)
		return false, nil
	}); err != nil {
		return nil, err
	}
	if err := k.DisplayNameModeration.Walk(ctx, nil, func(_ string, val types.DisplayNameModeration) (stop bool, err error) {
		genesis.DisplayNameModerationMap = append(genesis.DisplayNameModerationMap, val)
		return false, nil
	}); err != nil {
		return nil, err
	}
	if err := k.DisplayNameReportStake.Walk(ctx, nil, func(_ string, val types.DisplayNameReportStake) (stop bool, err error) {
		genesis.DisplayNameReportStakeMap = append(genesis.DisplayNameReportStakeMap, val)
		return false, nil
	}); err != nil {
		return nil, err
	}
	if err := k.DisplayNameAppealStake.Walk(ctx, nil, func(_ string, val types.DisplayNameAppealStake) (stop bool, err error) {
		genesis.DisplayNameAppealStakeMap = append(genesis.DisplayNameAppealStakeMap, val)
		return false, nil
	}); err != nil {
		return nil, err
	}

	err = k.Nomination.Walk(ctx, nil, func(_ uint64, val types.Nomination) (bool, error) {
		genesis.NominationList = append(genesis.NominationList, val)
		return false, nil
	})
	if err != nil {
		return nil, err
	}
	genesis.NominationCount, err = k.NominationSeq.Peek(ctx)
	if err != nil {
		return nil, err
	}
	err = k.NominationStake.Walk(ctx, nil, func(_ string, val types.NominationStake) (bool, error) {
		genesis.NominationStakeList = append(genesis.NominationStakeList, val)
		return false, nil
	})
	if err != nil {
		return nil, err
	}
	err = k.RetroRewardRecord.Walk(ctx, nil, func(_ string, val types.RetroRewardRecord) (bool, error) {
		genesis.RetroRewardRecordList = append(genesis.RetroRewardRecordList, val)
		return false, nil
	})
	if err != nil {
		return nil, err
	}
	// PruneCursor is not exported: after an import the rolling pruning
	// restarts from the first key, which only costs one extra pass.
	if err := k.TransitionAborts.Walk(ctx, nil, func(season, aborts uint64) (bool, error) {
		genesis.TransitionAbortList = append(genesis.TransitionAbortList, types.TransitionAbortCount{Season: season, Aborts: aborts})
		return false, nil
	}); err != nil {
		return nil, err
	}
	pending, err := k.PendingSeasonalPoolInit.Get(ctx)
	if err != nil && !errors.Is(err, collections.ErrNotFound) {
		return nil, err
	}
	genesis.PendingSeasonalPoolInit = pending

	if err := k.SeasonStatsCounters.Walk(ctx, nil, func(_ uint64, val types.SeasonStatsCounters) (bool, error) {
		genesis.SeasonStatsCountersList = append(genesis.SeasonStatsCountersList, val)
		return false, nil
	}); err != nil {
		return nil, err
	}
	if season, err := k.Season.Get(ctx); err == nil {
		cur, err := k.getCurrentSeasonStats(ctx)
		if err != nil {
			return nil, err
		}
		cur.Season = season.Number
		genesis.SeasonStatsCountersList = append(genesis.SeasonStatsCountersList, cur)
	}

	return genesis, nil
}

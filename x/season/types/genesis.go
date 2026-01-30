package types

import "fmt"

// DefaultGenesis returns the default genesis state
func DefaultGenesis() *GenesisState {
	return &GenesisState{
		Params: DefaultParams(),
		Season: nil, SeasonTransitionState: nil, TransitionRecoveryState: nil, NextSeasonInfo: nil, SeasonSnapshotMap: []SeasonSnapshot{}, MemberSeasonSnapshotMap: []MemberSeasonSnapshot{}, MemberProfileMap: []MemberProfile{}, MemberRegistrationMap: []MemberRegistration{}, AchievementMap: []Achievement{}, TitleMap: []Title{}, SeasonTitleEligibilityMap: []SeasonTitleEligibility{}, GuildList: []Guild{}, GuildMembershipMap: []GuildMembership{}, GuildInviteMap: []GuildInvite{}, QuestMap: []Quest{}, MemberQuestProgressMap: []MemberQuestProgress{}, EpochXpTrackerMap: []EpochXpTracker{}, VoteXpRecordMap: []VoteXpRecord{}, ForumXpCooldownMap: []ForumXpCooldown{}, DisplayNameModerationMap: []DisplayNameModeration{}, DisplayNameReportStakeMap: []DisplayNameReportStake{}, DisplayNameAppealStakeMap: []DisplayNameAppealStake{}}
}

// Validate performs basic genesis state validation returning an error upon any
// failure.
func (gs GenesisState) Validate() error {
	seasonSnapshotIndexMap := make(map[string]struct{})

	for _, elem := range gs.SeasonSnapshotMap {
		index := fmt.Sprint(elem.Season)
		if _, ok := seasonSnapshotIndexMap[index]; ok {
			return fmt.Errorf("duplicated index for seasonSnapshot")
		}
		seasonSnapshotIndexMap[index] = struct{}{}
	}
	memberSeasonSnapshotIndexMap := make(map[string]struct{})

	for _, elem := range gs.MemberSeasonSnapshotMap {
		index := fmt.Sprint(elem.SeasonAddress)
		if _, ok := memberSeasonSnapshotIndexMap[index]; ok {
			return fmt.Errorf("duplicated index for memberSeasonSnapshot")
		}
		memberSeasonSnapshotIndexMap[index] = struct{}{}
	}
	memberProfileIndexMap := make(map[string]struct{})

	for _, elem := range gs.MemberProfileMap {
		index := fmt.Sprint(elem.Address)
		if _, ok := memberProfileIndexMap[index]; ok {
			return fmt.Errorf("duplicated index for memberProfile")
		}
		memberProfileIndexMap[index] = struct{}{}
	}
	memberRegistrationIndexMap := make(map[string]struct{})

	for _, elem := range gs.MemberRegistrationMap {
		index := fmt.Sprint(elem.Member)
		if _, ok := memberRegistrationIndexMap[index]; ok {
			return fmt.Errorf("duplicated index for memberRegistration")
		}
		memberRegistrationIndexMap[index] = struct{}{}
	}
	achievementIndexMap := make(map[string]struct{})

	for _, elem := range gs.AchievementMap {
		index := fmt.Sprint(elem.AchievementId)
		if _, ok := achievementIndexMap[index]; ok {
			return fmt.Errorf("duplicated index for achievement")
		}
		achievementIndexMap[index] = struct{}{}
	}
	titleIndexMap := make(map[string]struct{})

	for _, elem := range gs.TitleMap {
		index := fmt.Sprint(elem.TitleId)
		if _, ok := titleIndexMap[index]; ok {
			return fmt.Errorf("duplicated index for title")
		}
		titleIndexMap[index] = struct{}{}
	}
	seasonTitleEligibilityIndexMap := make(map[string]struct{})

	for _, elem := range gs.SeasonTitleEligibilityMap {
		index := fmt.Sprint(elem.TitleSeason)
		if _, ok := seasonTitleEligibilityIndexMap[index]; ok {
			return fmt.Errorf("duplicated index for seasonTitleEligibility")
		}
		seasonTitleEligibilityIndexMap[index] = struct{}{}
	}
	guildIdMap := make(map[uint64]bool)
	guildCount := gs.GetGuildCount()
	for _, elem := range gs.GuildList {
		if _, ok := guildIdMap[elem.Id]; ok {
			return fmt.Errorf("duplicated id for guild")
		}
		if elem.Id >= guildCount {
			return fmt.Errorf("guild id should be lower or equal than the last id")
		}
		guildIdMap[elem.Id] = true
	}
	guildMembershipIndexMap := make(map[string]struct{})

	for _, elem := range gs.GuildMembershipMap {
		index := fmt.Sprint(elem.Member)
		if _, ok := guildMembershipIndexMap[index]; ok {
			return fmt.Errorf("duplicated index for guildMembership")
		}
		guildMembershipIndexMap[index] = struct{}{}
	}
	guildInviteIndexMap := make(map[string]struct{})

	for _, elem := range gs.GuildInviteMap {
		index := fmt.Sprint(elem.GuildInvitee)
		if _, ok := guildInviteIndexMap[index]; ok {
			return fmt.Errorf("duplicated index for guildInvite")
		}
		guildInviteIndexMap[index] = struct{}{}
	}
	questIndexMap := make(map[string]struct{})

	for _, elem := range gs.QuestMap {
		index := fmt.Sprint(elem.QuestId)
		if _, ok := questIndexMap[index]; ok {
			return fmt.Errorf("duplicated index for quest")
		}
		questIndexMap[index] = struct{}{}
	}
	memberQuestProgressIndexMap := make(map[string]struct{})

	for _, elem := range gs.MemberQuestProgressMap {
		index := fmt.Sprint(elem.MemberQuest)
		if _, ok := memberQuestProgressIndexMap[index]; ok {
			return fmt.Errorf("duplicated index for memberQuestProgress")
		}
		memberQuestProgressIndexMap[index] = struct{}{}
	}
	epochXpTrackerIndexMap := make(map[string]struct{})

	for _, elem := range gs.EpochXpTrackerMap {
		index := fmt.Sprint(elem.MemberEpoch)
		if _, ok := epochXpTrackerIndexMap[index]; ok {
			return fmt.Errorf("duplicated index for epochXpTracker")
		}
		epochXpTrackerIndexMap[index] = struct{}{}
	}
	voteXpRecordIndexMap := make(map[string]struct{})

	for _, elem := range gs.VoteXpRecordMap {
		index := fmt.Sprint(elem.SeasonMemberProposal)
		if _, ok := voteXpRecordIndexMap[index]; ok {
			return fmt.Errorf("duplicated index for voteXpRecord")
		}
		voteXpRecordIndexMap[index] = struct{}{}
	}
	forumXpCooldownIndexMap := make(map[string]struct{})

	for _, elem := range gs.ForumXpCooldownMap {
		index := fmt.Sprint(elem.BeneficiaryActor)
		if _, ok := forumXpCooldownIndexMap[index]; ok {
			return fmt.Errorf("duplicated index for forumXpCooldown")
		}
		forumXpCooldownIndexMap[index] = struct{}{}
	}
	displayNameModerationIndexMap := make(map[string]struct{})

	for _, elem := range gs.DisplayNameModerationMap {
		index := fmt.Sprint(elem.Member)
		if _, ok := displayNameModerationIndexMap[index]; ok {
			return fmt.Errorf("duplicated index for displayNameModeration")
		}
		displayNameModerationIndexMap[index] = struct{}{}
	}
	displayNameReportStakeIndexMap := make(map[string]struct{})

	for _, elem := range gs.DisplayNameReportStakeMap {
		index := fmt.Sprint(elem.ChallengeId)
		if _, ok := displayNameReportStakeIndexMap[index]; ok {
			return fmt.Errorf("duplicated index for displayNameReportStake")
		}
		displayNameReportStakeIndexMap[index] = struct{}{}
	}
	displayNameAppealStakeIndexMap := make(map[string]struct{})

	for _, elem := range gs.DisplayNameAppealStakeMap {
		index := fmt.Sprint(elem.ChallengeId)
		if _, ok := displayNameAppealStakeIndexMap[index]; ok {
			return fmt.Errorf("duplicated index for displayNameAppealStake")
		}
		displayNameAppealStakeIndexMap[index] = struct{}{}
	}

	return gs.Params.Validate()
}

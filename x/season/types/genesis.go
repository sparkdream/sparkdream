package types

import (
	"fmt"

	"cosmossdk.io/math"
)

// DefaultGenesis returns the default genesis state
func DefaultGenesis() *GenesisState {
	return &GenesisState{
		Params: DefaultParams(),
		Season: nil, SeasonTransitionState: nil, TransitionRecoveryState: nil, NextSeasonInfo: nil, SeasonSnapshotMap: []SeasonSnapshot{}, MemberSeasonSnapshotMap: []MemberSeasonSnapshot{}, MemberProfileMap: []MemberProfile{}, MemberRegistrationMap: []MemberRegistration{}, AchievementMap: []Achievement{}, TitleMap: []Title{}, SeasonTitleEligibilityMap: []SeasonTitleEligibility{}, GuildList: []Guild{}, GuildMembershipMap: []GuildMembership{}, GuildInviteMap: []GuildInvite{}, QuestMap: []Quest{}, MemberQuestProgressMap: []MemberQuestProgress{}, EpochXpTrackerMap: []EpochXpTracker{}, VoteXpRecordMap: []VoteXpRecord{}, ForumXpCooldownMap: []ForumXpCooldown{}, DisplayNameModerationMap: []DisplayNameModeration{}, DisplayNameReportStakeMap: []DisplayNameReportStake{}, DisplayNameAppealStakeMap: []DisplayNameAppealStake{}, SeasonHistoryList: []Season{}}
}

// Validate performs basic genesis state validation returning an error upon any
// failure.
func (gs GenesisState) Validate() error {
	// Completed seasons: unique numbers, each below the live season's,
	// status COMPLETED.
	seasonHistoryNumbers := make(map[uint64]struct{})
	for _, elem := range gs.SeasonHistoryList {
		if _, ok := seasonHistoryNumbers[elem.Number]; ok {
			return fmt.Errorf("duplicated season number %d in season_history_list", elem.Number)
		}
		seasonHistoryNumbers[elem.Number] = struct{}{}
		if elem.Status != SeasonStatus_SEASON_STATUS_COMPLETED {
			return fmt.Errorf("season %d in season_history_list has status %s, want COMPLETED", elem.Number, elem.Status)
		}
		if gs.Season != nil && elem.Number >= gs.Season.Number {
			return fmt.Errorf("season %d in season_history_list is not below the live season %d", elem.Number, gs.Season.Number)
		}
	}
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
		// Guild ids are GuildSeq.Next()+1 (0 means "no guild"), so the
		// highest id equals the exported count (SEA-25: an export with
		// guilds must validate).
		if elem.Id > guildCount {
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
		// Store key is "<guild_id>:<invitee>" (SEA-25): one member may
		// hold invites from several guilds.
		if elem.GuildId == 0 {
			return fmt.Errorf("guild invite for %s has no guild_id", elem.GuildInvitee)
		}
		index := fmt.Sprintf("%d:%s", elem.GuildId, elem.GuildInvitee)
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
		if elem.XpReward > HardMaxQuestXpReward {
			return fmt.Errorf("quest %s xp_reward %d exceeds the hard ceiling %d", elem.QuestId, elem.XpReward, HardMaxQuestXpReward)
		}
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

	if err := gs.validateRelations(); err != nil {
		return err
	}

	return gs.Params.Validate()
}

// validateRelations checks cross-record consistency that the per-map
// duplicate checks cannot see (SEA-36): memberships and invites point at
// existing guilds, nomination stakes at existing nominations and sum to
// their total_staked, display-name stakes at active moderations, and the
// transition phase is a defined value.
func (gs GenesisState) validateRelations() error {
	guilds := make(map[uint64]Guild, len(gs.GuildList))
	for _, g := range gs.GuildList {
		guilds[g.Id] = g
	}
	for _, m := range gs.GuildMembershipMap {
		if m.GuildId == 0 || m.LeftEpoch != 0 {
			continue
		}
		g, ok := guilds[m.GuildId]
		if !ok {
			return fmt.Errorf("guild membership of %s references missing guild %d", m.Member, m.GuildId)
		}
		if g.Status == GuildStatus_GUILD_STATUS_DISSOLVED {
			return fmt.Errorf("guild membership of %s references dissolved guild %d", m.Member, m.GuildId)
		}
	}
	for _, inv := range gs.GuildInviteMap {
		if _, ok := guilds[inv.GuildId]; !ok {
			return fmt.Errorf("guild invite for %s references missing guild %d", inv.GuildInvitee, inv.GuildId)
		}
	}

	noms := make(map[uint64]Nomination, len(gs.NominationList))
	for _, n := range gs.NominationList {
		if _, dup := noms[n.Id]; dup {
			return fmt.Errorf("duplicated nomination id %d", n.Id)
		}
		noms[n.Id] = n
	}
	sums := map[uint64]math.LegacyDec{}
	for _, st := range gs.NominationStakeList {
		if _, ok := noms[st.NominationId]; !ok {
			return fmt.Errorf("nomination stake of %s references missing nomination %d", st.Staker, st.NominationId)
		}
		if st.Amount.IsNil() || st.Amount.IsNegative() {
			return fmt.Errorf("nomination stake of %s on %d has an invalid amount", st.Staker, st.NominationId)
		}
		cur, ok := sums[st.NominationId]
		if !ok {
			cur = math.LegacyZeroDec()
		}
		sums[st.NominationId] = cur.Add(st.Amount)
	}
	for id, sum := range sums {
		if total := noms[id].TotalStaked; total.IsNil() || !total.Equal(sum) {
			return fmt.Errorf("nomination %d total_staked %v does not match its stakes (%s)", id, total, sum)
		}
	}

	openReports := map[string]bool{}
	openAppeals := map[string]bool{}
	for _, m := range gs.DisplayNameModerationMap {
		if !m.Active {
			continue
		}
		openReports[fmt.Sprintf("dn:%s:%d", m.Member, m.ModeratedAt)] = true
		if m.AppealChallengeId != "" {
			openAppeals[m.AppealChallengeId] = true
		}
	}
	for _, st := range gs.DisplayNameReportStakeMap {
		if !openReports[st.ChallengeId] {
			return fmt.Errorf("display name report stake %s has no active moderation", st.ChallengeId)
		}
	}
	for _, st := range gs.DisplayNameAppealStakeMap {
		if !openAppeals[st.ChallengeId] {
			return fmt.Errorf("display name appeal stake %s has no active appealed moderation", st.ChallengeId)
		}
	}

	if ts := gs.SeasonTransitionState; ts != nil {
		if _, ok := TransitionPhase_name[int32(ts.Phase)]; !ok {
			return fmt.Errorf("season transition phase %d is not defined", ts.Phase)
		}
		// The flag is the source of truth for IsInMaintenanceMode and is
		// re-derived only when a batch commits, so a stale imported value
		// would leave reputation moving under a SNAPSHOT that keeps
		// failing (SEA-203).
		if ts.MaintenanceMode != ts.Phase.IsMaintenance() {
			return fmt.Errorf("season transition maintenance_mode %t does not match phase %s", ts.MaintenanceMode, ts.Phase)
		}
	}

	statsSeasons := map[uint64]bool{}
	for _, c := range gs.SeasonStatsCountersList {
		if statsSeasons[c.Season] {
			return fmt.Errorf("duplicated season stats counters for season %d", c.Season)
		}
		statsSeasons[c.Season] = true
	}
	return nil
}

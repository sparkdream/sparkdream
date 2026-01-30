package keeper_test

import (
	"testing"

	"sparkdream/x/season/types"

	"github.com/stretchr/testify/require"
)

func TestGenesis(t *testing.T) {
	genesisState := types.GenesisState{
		Params: types.DefaultParams(),

		Season: &types.Season{Number: 17,
			Name:                 "53",
			Theme:                "47",
			StartBlock:           25,
			EndBlock:             32,
			Status:               35,
			ExtensionsCount:      18,
			TotalExtensionEpochs: 30,
			OriginalEndBlock:     21,
		}, SeasonTransitionState: &types.SeasonTransitionState{Phase: 93,
			ProcessedCount:  34,
			TotalCount:      76,
			LastProcessed:   "39",
			TransitionStart: 86,
			MaintenanceMode: true,
		}, TransitionRecoveryState: &types.TransitionRecoveryState{LastAttemptBlock: 70,
			FailedPhase:  7,
			FailureCount: 95,
			LastError:    "72",
			RecoveryMode: false,
		}, NextSeasonInfo: &types.NextSeasonInfo{Name: "26",
			Theme: "95",
		}, SeasonSnapshotMap: []types.SeasonSnapshot{{Season: 0}, {Season: 1}}, MemberSeasonSnapshotMap: []types.MemberSeasonSnapshot{{SeasonAddress: "0"}, {SeasonAddress: "1"}}, MemberProfileMap: []types.MemberProfile{{Address: "0"}, {Address: "1"}}, MemberRegistrationMap: []types.MemberRegistration{{Member: "0"}, {Member: "1"}}, AchievementMap: []types.Achievement{{AchievementId: "0"}, {AchievementId: "1"}}, TitleMap: []types.Title{{TitleId: "0"}, {TitleId: "1"}}, SeasonTitleEligibilityMap: []types.SeasonTitleEligibility{{TitleSeason: 0}, {TitleSeason: 1}}, GuildList: []types.Guild{{Id: 0}, {Id: 1}},
		GuildCount:         2,
		GuildMembershipMap: []types.GuildMembership{{Member: "0"}, {Member: "1"}}, GuildInviteMap: []types.GuildInvite{{GuildInvitee: "0"}, {GuildInvitee: "1"}}, QuestMap: []types.Quest{{QuestId: "0"}, {QuestId: "1"}}, MemberQuestProgressMap: []types.MemberQuestProgress{{MemberQuest: "0"}, {MemberQuest: "1"}}, EpochXpTrackerMap: []types.EpochXpTracker{{MemberEpoch: "0"}, {MemberEpoch: "1"}}, VoteXpRecordMap: []types.VoteXpRecord{{SeasonMemberProposal: "0"}, {SeasonMemberProposal: "1"}}, ForumXpCooldownMap: []types.ForumXpCooldown{{BeneficiaryActor: "0"}, {BeneficiaryActor: "1"}}, DisplayNameModerationMap: []types.DisplayNameModeration{{Member: "0"}, {Member: "1"}}, DisplayNameReportStakeMap: []types.DisplayNameReportStake{{ChallengeId: "0"}, {ChallengeId: "1"}}, DisplayNameAppealStakeMap: []types.DisplayNameAppealStake{{ChallengeId: "0"}, {ChallengeId: "1"}}}
	f := initFixture(t)
	err := f.keeper.InitGenesis(f.ctx, genesisState)
	require.NoError(t, err)
	got, err := f.keeper.ExportGenesis(f.ctx)
	require.NoError(t, err)
	require.NotNil(t, got)

	require.EqualExportedValues(t, genesisState.Params, got.Params)
	require.EqualExportedValues(t, genesisState.Season, got.Season)
	require.EqualExportedValues(t, genesisState.SeasonTransitionState, got.SeasonTransitionState)
	require.EqualExportedValues(t, genesisState.TransitionRecoveryState, got.TransitionRecoveryState)
	require.EqualExportedValues(t, genesisState.NextSeasonInfo, got.NextSeasonInfo)
	require.EqualExportedValues(t, genesisState.SeasonSnapshotMap, got.SeasonSnapshotMap)
	require.EqualExportedValues(t, genesisState.MemberSeasonSnapshotMap, got.MemberSeasonSnapshotMap)
	require.EqualExportedValues(t, genesisState.MemberProfileMap, got.MemberProfileMap)
	require.EqualExportedValues(t, genesisState.MemberRegistrationMap, got.MemberRegistrationMap)
	require.EqualExportedValues(t, genesisState.AchievementMap, got.AchievementMap)
	require.EqualExportedValues(t, genesisState.TitleMap, got.TitleMap)
	require.EqualExportedValues(t, genesisState.SeasonTitleEligibilityMap, got.SeasonTitleEligibilityMap)
	require.EqualExportedValues(t, genesisState.GuildList, got.GuildList)
	require.Equal(t, genesisState.GuildCount, got.GuildCount)
	require.EqualExportedValues(t, genesisState.GuildMembershipMap, got.GuildMembershipMap)
	require.EqualExportedValues(t, genesisState.GuildInviteMap, got.GuildInviteMap)
	require.EqualExportedValues(t, genesisState.QuestMap, got.QuestMap)
	require.EqualExportedValues(t, genesisState.MemberQuestProgressMap, got.MemberQuestProgressMap)
	require.EqualExportedValues(t, genesisState.EpochXpTrackerMap, got.EpochXpTrackerMap)
	require.EqualExportedValues(t, genesisState.VoteXpRecordMap, got.VoteXpRecordMap)
	require.EqualExportedValues(t, genesisState.ForumXpCooldownMap, got.ForumXpCooldownMap)
	require.EqualExportedValues(t, genesisState.DisplayNameModerationMap, got.DisplayNameModerationMap)
	require.EqualExportedValues(t, genesisState.DisplayNameReportStakeMap, got.DisplayNameReportStakeMap)
	require.EqualExportedValues(t, genesisState.DisplayNameAppealStakeMap, got.DisplayNameAppealStakeMap)

}

package types_test

import (
	"testing"

	"sparkdream/x/season/types"

	"github.com/stretchr/testify/require"
)

func TestGenesisState_Validate(t *testing.T) {
	tests := []struct {
		desc     string
		genState *types.GenesisState
		valid    bool
	}{
		{
			desc:     "default is valid",
			genState: types.DefaultGenesis(),
			valid:    true,
		},
		{
			desc: "valid genesis state",
			genState: &types.GenesisState{

				Season: &types.Season{Number: 74,
					Name:                 "40",
					Theme:                "43",
					StartBlock:           39,
					EndBlock:             80,
					Status:               50,
					ExtensionsCount:      28,
					TotalExtensionEpochs: 94,
					OriginalEndBlock:     91,
				}, SeasonTransitionState: &types.SeasonTransitionState{Phase: 24,
					ProcessedCount:  22,
					TotalCount:      42,
					LastProcessed:   "13",
					TransitionStart: 26,
					MaintenanceMode: false,
				}, TransitionRecoveryState: &types.TransitionRecoveryState{LastAttemptBlock: 84,
					FailedPhase:  66,
					FailureCount: 5,
					LastError:    "50",
					RecoveryMode: false,
				}, NextSeasonInfo: &types.NextSeasonInfo{Name: "5",
					Theme: "29",
				}, SeasonSnapshotMap: []types.SeasonSnapshot{{Season: 0}, {Season: 1}}, MemberSeasonSnapshotMap: []types.MemberSeasonSnapshot{{SeasonAddress: "0"}, {SeasonAddress: "1"}}, MemberProfileMap: []types.MemberProfile{{Address: "0"}, {Address: "1"}}, MemberRegistrationMap: []types.MemberRegistration{{Member: "0"}, {Member: "1"}}, AchievementMap: []types.Achievement{{AchievementId: "0"}, {AchievementId: "1"}}, TitleMap: []types.Title{{TitleId: "0"}, {TitleId: "1"}}, SeasonTitleEligibilityMap: []types.SeasonTitleEligibility{{TitleSeason: 0}, {TitleSeason: 1}}, GuildList: []types.Guild{{Id: 0}, {Id: 1}}, GuildCount: 2,
				GuildMembershipMap: []types.GuildMembership{{Member: "0"}, {Member: "1"}}, GuildInviteMap: []types.GuildInvite{{GuildInvitee: "0"}, {GuildInvitee: "1"}}, QuestMap: []types.Quest{{QuestId: "0"}, {QuestId: "1"}}, MemberQuestProgressMap: []types.MemberQuestProgress{{MemberQuest: "0"}, {MemberQuest: "1"}}, EpochXpTrackerMap: []types.EpochXpTracker{{MemberEpoch: "0"}, {MemberEpoch: "1"}}, VoteXpRecordMap: []types.VoteXpRecord{{SeasonMemberProposal: "0"}, {SeasonMemberProposal: "1"}}, ForumXpCooldownMap: []types.ForumXpCooldown{{BeneficiaryActor: "0"}, {BeneficiaryActor: "1"}}, DisplayNameModerationMap: []types.DisplayNameModeration{{Member: "0"}, {Member: "1"}}, DisplayNameReportStakeMap: []types.DisplayNameReportStake{{ChallengeId: "0"}, {ChallengeId: "1"}}, DisplayNameAppealStakeMap: []types.DisplayNameAppealStake{{ChallengeId: "0"}, {ChallengeId: "1"}}}, valid: true,
		}, {
			desc: "duplicated seasonSnapshot",
			genState: &types.GenesisState{
				SeasonSnapshotMap: []types.SeasonSnapshot{
					{
						Season: 0,
					},
					{
						Season: 0,
					},
				},
				MemberSeasonSnapshotMap: []types.MemberSeasonSnapshot{{SeasonAddress: "0"}, {SeasonAddress: "1"}}, MemberProfileMap: []types.MemberProfile{{Address: "0"}, {Address: "1"}}, MemberRegistrationMap: []types.MemberRegistration{{Member: "0"}, {Member: "1"}}, AchievementMap: []types.Achievement{{AchievementId: "0"}, {AchievementId: "1"}}, TitleMap: []types.Title{{TitleId: "0"}, {TitleId: "1"}}, SeasonTitleEligibilityMap: []types.SeasonTitleEligibility{{TitleSeason: 0}, {TitleSeason: 1}}, GuildList: []types.Guild{{Id: 0}, {Id: 1}}, GuildCount: 2,
				GuildMembershipMap: []types.GuildMembership{{Member: "0"}, {Member: "1"}}, GuildInviteMap: []types.GuildInvite{{GuildInvitee: "0"}, {GuildInvitee: "1"}}, QuestMap: []types.Quest{{QuestId: "0"}, {QuestId: "1"}}, MemberQuestProgressMap: []types.MemberQuestProgress{{MemberQuest: "0"}, {MemberQuest: "1"}}, EpochXpTrackerMap: []types.EpochXpTracker{{MemberEpoch: "0"}, {MemberEpoch: "1"}}, VoteXpRecordMap: []types.VoteXpRecord{{SeasonMemberProposal: "0"}, {SeasonMemberProposal: "1"}}, ForumXpCooldownMap: []types.ForumXpCooldown{{BeneficiaryActor: "0"}, {BeneficiaryActor: "1"}}, DisplayNameModerationMap: []types.DisplayNameModeration{{Member: "0"}, {Member: "1"}}, DisplayNameReportStakeMap: []types.DisplayNameReportStake{{ChallengeId: "0"}, {ChallengeId: "1"}}, DisplayNameAppealStakeMap: []types.DisplayNameAppealStake{{ChallengeId: "0"}, {ChallengeId: "1"}}}, valid: false,
		}, {
			desc: "duplicated memberSeasonSnapshot",
			genState: &types.GenesisState{
				MemberSeasonSnapshotMap: []types.MemberSeasonSnapshot{
					{
						SeasonAddress: "0",
					},
					{
						SeasonAddress: "0",
					},
				},
				MemberProfileMap: []types.MemberProfile{{Address: "0"}, {Address: "1"}}, MemberRegistrationMap: []types.MemberRegistration{{Member: "0"}, {Member: "1"}}, AchievementMap: []types.Achievement{{AchievementId: "0"}, {AchievementId: "1"}}, TitleMap: []types.Title{{TitleId: "0"}, {TitleId: "1"}}, SeasonTitleEligibilityMap: []types.SeasonTitleEligibility{{TitleSeason: 0}, {TitleSeason: 1}}, GuildList: []types.Guild{{Id: 0}, {Id: 1}}, GuildCount: 2,
				GuildMembershipMap: []types.GuildMembership{{Member: "0"}, {Member: "1"}}, GuildInviteMap: []types.GuildInvite{{GuildInvitee: "0"}, {GuildInvitee: "1"}}, QuestMap: []types.Quest{{QuestId: "0"}, {QuestId: "1"}}, MemberQuestProgressMap: []types.MemberQuestProgress{{MemberQuest: "0"}, {MemberQuest: "1"}}, EpochXpTrackerMap: []types.EpochXpTracker{{MemberEpoch: "0"}, {MemberEpoch: "1"}}, VoteXpRecordMap: []types.VoteXpRecord{{SeasonMemberProposal: "0"}, {SeasonMemberProposal: "1"}}, ForumXpCooldownMap: []types.ForumXpCooldown{{BeneficiaryActor: "0"}, {BeneficiaryActor: "1"}}, DisplayNameModerationMap: []types.DisplayNameModeration{{Member: "0"}, {Member: "1"}}, DisplayNameReportStakeMap: []types.DisplayNameReportStake{{ChallengeId: "0"}, {ChallengeId: "1"}}, DisplayNameAppealStakeMap: []types.DisplayNameAppealStake{{ChallengeId: "0"}, {ChallengeId: "1"}}}, valid: false,
		}, {
			desc: "duplicated memberProfile",
			genState: &types.GenesisState{
				MemberProfileMap: []types.MemberProfile{
					{
						Address: "0",
					},
					{
						Address: "0",
					},
				},
				MemberRegistrationMap: []types.MemberRegistration{{Member: "0"}, {Member: "1"}}, AchievementMap: []types.Achievement{{AchievementId: "0"}, {AchievementId: "1"}}, TitleMap: []types.Title{{TitleId: "0"}, {TitleId: "1"}}, SeasonTitleEligibilityMap: []types.SeasonTitleEligibility{{TitleSeason: 0}, {TitleSeason: 1}}, GuildList: []types.Guild{{Id: 0}, {Id: 1}}, GuildCount: 2,
				GuildMembershipMap: []types.GuildMembership{{Member: "0"}, {Member: "1"}}, GuildInviteMap: []types.GuildInvite{{GuildInvitee: "0"}, {GuildInvitee: "1"}}, QuestMap: []types.Quest{{QuestId: "0"}, {QuestId: "1"}}, MemberQuestProgressMap: []types.MemberQuestProgress{{MemberQuest: "0"}, {MemberQuest: "1"}}, EpochXpTrackerMap: []types.EpochXpTracker{{MemberEpoch: "0"}, {MemberEpoch: "1"}}, VoteXpRecordMap: []types.VoteXpRecord{{SeasonMemberProposal: "0"}, {SeasonMemberProposal: "1"}}, ForumXpCooldownMap: []types.ForumXpCooldown{{BeneficiaryActor: "0"}, {BeneficiaryActor: "1"}}, DisplayNameModerationMap: []types.DisplayNameModeration{{Member: "0"}, {Member: "1"}}, DisplayNameReportStakeMap: []types.DisplayNameReportStake{{ChallengeId: "0"}, {ChallengeId: "1"}}, DisplayNameAppealStakeMap: []types.DisplayNameAppealStake{{ChallengeId: "0"}, {ChallengeId: "1"}}}, valid: false,
		}, {
			desc: "duplicated memberRegistration",
			genState: &types.GenesisState{
				MemberRegistrationMap: []types.MemberRegistration{
					{
						Member: "0",
					},
					{
						Member: "0",
					},
				},
				AchievementMap: []types.Achievement{{AchievementId: "0"}, {AchievementId: "1"}}, TitleMap: []types.Title{{TitleId: "0"}, {TitleId: "1"}}, SeasonTitleEligibilityMap: []types.SeasonTitleEligibility{{TitleSeason: 0}, {TitleSeason: 1}}, GuildList: []types.Guild{{Id: 0}, {Id: 1}}, GuildCount: 2,
				GuildMembershipMap: []types.GuildMembership{{Member: "0"}, {Member: "1"}}, GuildInviteMap: []types.GuildInvite{{GuildInvitee: "0"}, {GuildInvitee: "1"}}, QuestMap: []types.Quest{{QuestId: "0"}, {QuestId: "1"}}, MemberQuestProgressMap: []types.MemberQuestProgress{{MemberQuest: "0"}, {MemberQuest: "1"}}, EpochXpTrackerMap: []types.EpochXpTracker{{MemberEpoch: "0"}, {MemberEpoch: "1"}}, VoteXpRecordMap: []types.VoteXpRecord{{SeasonMemberProposal: "0"}, {SeasonMemberProposal: "1"}}, ForumXpCooldownMap: []types.ForumXpCooldown{{BeneficiaryActor: "0"}, {BeneficiaryActor: "1"}}, DisplayNameModerationMap: []types.DisplayNameModeration{{Member: "0"}, {Member: "1"}}, DisplayNameReportStakeMap: []types.DisplayNameReportStake{{ChallengeId: "0"}, {ChallengeId: "1"}}, DisplayNameAppealStakeMap: []types.DisplayNameAppealStake{{ChallengeId: "0"}, {ChallengeId: "1"}}}, valid: false,
		}, {
			desc: "duplicated achievement",
			genState: &types.GenesisState{
				AchievementMap: []types.Achievement{
					{
						AchievementId: "0",
					},
					{
						AchievementId: "0",
					},
				},
				TitleMap: []types.Title{{TitleId: "0"}, {TitleId: "1"}}, SeasonTitleEligibilityMap: []types.SeasonTitleEligibility{{TitleSeason: 0}, {TitleSeason: 1}}, GuildList: []types.Guild{{Id: 0}, {Id: 1}}, GuildCount: 2,
				GuildMembershipMap: []types.GuildMembership{{Member: "0"}, {Member: "1"}}, GuildInviteMap: []types.GuildInvite{{GuildInvitee: "0"}, {GuildInvitee: "1"}}, QuestMap: []types.Quest{{QuestId: "0"}, {QuestId: "1"}}, MemberQuestProgressMap: []types.MemberQuestProgress{{MemberQuest: "0"}, {MemberQuest: "1"}}, EpochXpTrackerMap: []types.EpochXpTracker{{MemberEpoch: "0"}, {MemberEpoch: "1"}}, VoteXpRecordMap: []types.VoteXpRecord{{SeasonMemberProposal: "0"}, {SeasonMemberProposal: "1"}}, ForumXpCooldownMap: []types.ForumXpCooldown{{BeneficiaryActor: "0"}, {BeneficiaryActor: "1"}}, DisplayNameModerationMap: []types.DisplayNameModeration{{Member: "0"}, {Member: "1"}}, DisplayNameReportStakeMap: []types.DisplayNameReportStake{{ChallengeId: "0"}, {ChallengeId: "1"}}, DisplayNameAppealStakeMap: []types.DisplayNameAppealStake{{ChallengeId: "0"}, {ChallengeId: "1"}}}, valid: false,
		}, {
			desc: "duplicated title",
			genState: &types.GenesisState{
				TitleMap: []types.Title{
					{
						TitleId: "0",
					},
					{
						TitleId: "0",
					},
				},
				SeasonTitleEligibilityMap: []types.SeasonTitleEligibility{{TitleSeason: 0}, {TitleSeason: 1}}, GuildList: []types.Guild{{Id: 0}, {Id: 1}}, GuildCount: 2,
				GuildMembershipMap: []types.GuildMembership{{Member: "0"}, {Member: "1"}}, GuildInviteMap: []types.GuildInvite{{GuildInvitee: "0"}, {GuildInvitee: "1"}}, QuestMap: []types.Quest{{QuestId: "0"}, {QuestId: "1"}}, MemberQuestProgressMap: []types.MemberQuestProgress{{MemberQuest: "0"}, {MemberQuest: "1"}}, EpochXpTrackerMap: []types.EpochXpTracker{{MemberEpoch: "0"}, {MemberEpoch: "1"}}, VoteXpRecordMap: []types.VoteXpRecord{{SeasonMemberProposal: "0"}, {SeasonMemberProposal: "1"}}, ForumXpCooldownMap: []types.ForumXpCooldown{{BeneficiaryActor: "0"}, {BeneficiaryActor: "1"}}, DisplayNameModerationMap: []types.DisplayNameModeration{{Member: "0"}, {Member: "1"}}, DisplayNameReportStakeMap: []types.DisplayNameReportStake{{ChallengeId: "0"}, {ChallengeId: "1"}}, DisplayNameAppealStakeMap: []types.DisplayNameAppealStake{{ChallengeId: "0"}, {ChallengeId: "1"}}}, valid: false,
		}, {
			desc: "duplicated seasonTitleEligibility",
			genState: &types.GenesisState{
				SeasonTitleEligibilityMap: []types.SeasonTitleEligibility{
					{
						TitleSeason: 0,
					},
					{
						TitleSeason: 0,
					},
				},
				GuildList: []types.Guild{{Id: 0}, {Id: 1}}, GuildCount: 2,
				GuildMembershipMap: []types.GuildMembership{{Member: "0"}, {Member: "1"}}, GuildInviteMap: []types.GuildInvite{{GuildInvitee: "0"}, {GuildInvitee: "1"}}, QuestMap: []types.Quest{{QuestId: "0"}, {QuestId: "1"}}, MemberQuestProgressMap: []types.MemberQuestProgress{{MemberQuest: "0"}, {MemberQuest: "1"}}, EpochXpTrackerMap: []types.EpochXpTracker{{MemberEpoch: "0"}, {MemberEpoch: "1"}}, VoteXpRecordMap: []types.VoteXpRecord{{SeasonMemberProposal: "0"}, {SeasonMemberProposal: "1"}}, ForumXpCooldownMap: []types.ForumXpCooldown{{BeneficiaryActor: "0"}, {BeneficiaryActor: "1"}}, DisplayNameModerationMap: []types.DisplayNameModeration{{Member: "0"}, {Member: "1"}}, DisplayNameReportStakeMap: []types.DisplayNameReportStake{{ChallengeId: "0"}, {ChallengeId: "1"}}, DisplayNameAppealStakeMap: []types.DisplayNameAppealStake{{ChallengeId: "0"}, {ChallengeId: "1"}}}, valid: false,
		}, {
			desc: "duplicated guild",
			genState: &types.GenesisState{
				GuildList: []types.Guild{
					{
						Id: 0,
					},
					{
						Id: 0,
					},
				},
				GuildMembershipMap: []types.GuildMembership{{Member: "0"}, {Member: "1"}}, GuildInviteMap: []types.GuildInvite{{GuildInvitee: "0"}, {GuildInvitee: "1"}}, QuestMap: []types.Quest{{QuestId: "0"}, {QuestId: "1"}}, MemberQuestProgressMap: []types.MemberQuestProgress{{MemberQuest: "0"}, {MemberQuest: "1"}}, EpochXpTrackerMap: []types.EpochXpTracker{{MemberEpoch: "0"}, {MemberEpoch: "1"}}, VoteXpRecordMap: []types.VoteXpRecord{{SeasonMemberProposal: "0"}, {SeasonMemberProposal: "1"}}, ForumXpCooldownMap: []types.ForumXpCooldown{{BeneficiaryActor: "0"}, {BeneficiaryActor: "1"}}, DisplayNameModerationMap: []types.DisplayNameModeration{{Member: "0"}, {Member: "1"}}, DisplayNameReportStakeMap: []types.DisplayNameReportStake{{ChallengeId: "0"}, {ChallengeId: "1"}}, DisplayNameAppealStakeMap: []types.DisplayNameAppealStake{{ChallengeId: "0"}, {ChallengeId: "1"}}},
			valid: false,
		}, {
			desc: "invalid guild count",
			genState: &types.GenesisState{
				GuildList: []types.Guild{
					{
						Id: 1,
					},
				},
				GuildCount:         0,
				GuildMembershipMap: []types.GuildMembership{{Member: "0"}, {Member: "1"}}, GuildInviteMap: []types.GuildInvite{{GuildInvitee: "0"}, {GuildInvitee: "1"}}, QuestMap: []types.Quest{{QuestId: "0"}, {QuestId: "1"}}, MemberQuestProgressMap: []types.MemberQuestProgress{{MemberQuest: "0"}, {MemberQuest: "1"}}, EpochXpTrackerMap: []types.EpochXpTracker{{MemberEpoch: "0"}, {MemberEpoch: "1"}}, VoteXpRecordMap: []types.VoteXpRecord{{SeasonMemberProposal: "0"}, {SeasonMemberProposal: "1"}}, ForumXpCooldownMap: []types.ForumXpCooldown{{BeneficiaryActor: "0"}, {BeneficiaryActor: "1"}}, DisplayNameModerationMap: []types.DisplayNameModeration{{Member: "0"}, {Member: "1"}}, DisplayNameReportStakeMap: []types.DisplayNameReportStake{{ChallengeId: "0"}, {ChallengeId: "1"}}, DisplayNameAppealStakeMap: []types.DisplayNameAppealStake{{ChallengeId: "0"}, {ChallengeId: "1"}}},
			valid: false,
		}, {
			desc: "duplicated guildMembership",
			genState: &types.GenesisState{
				GuildMembershipMap: []types.GuildMembership{
					{
						Member: "0",
					},
					{
						Member: "0",
					},
				},
				GuildInviteMap: []types.GuildInvite{{GuildInvitee: "0"}, {GuildInvitee: "1"}}, QuestMap: []types.Quest{{QuestId: "0"}, {QuestId: "1"}}, MemberQuestProgressMap: []types.MemberQuestProgress{{MemberQuest: "0"}, {MemberQuest: "1"}}, EpochXpTrackerMap: []types.EpochXpTracker{{MemberEpoch: "0"}, {MemberEpoch: "1"}}, VoteXpRecordMap: []types.VoteXpRecord{{SeasonMemberProposal: "0"}, {SeasonMemberProposal: "1"}}, ForumXpCooldownMap: []types.ForumXpCooldown{{BeneficiaryActor: "0"}, {BeneficiaryActor: "1"}}, DisplayNameModerationMap: []types.DisplayNameModeration{{Member: "0"}, {Member: "1"}}, DisplayNameReportStakeMap: []types.DisplayNameReportStake{{ChallengeId: "0"}, {ChallengeId: "1"}}, DisplayNameAppealStakeMap: []types.DisplayNameAppealStake{{ChallengeId: "0"}, {ChallengeId: "1"}}},
			valid: false,
		}, {
			desc: "duplicated guildInvite",
			genState: &types.GenesisState{
				GuildInviteMap: []types.GuildInvite{
					{
						GuildInvitee: "0",
					},
					{
						GuildInvitee: "0",
					},
				},
				QuestMap: []types.Quest{{QuestId: "0"}, {QuestId: "1"}}, MemberQuestProgressMap: []types.MemberQuestProgress{{MemberQuest: "0"}, {MemberQuest: "1"}}, EpochXpTrackerMap: []types.EpochXpTracker{{MemberEpoch: "0"}, {MemberEpoch: "1"}}, VoteXpRecordMap: []types.VoteXpRecord{{SeasonMemberProposal: "0"}, {SeasonMemberProposal: "1"}}, ForumXpCooldownMap: []types.ForumXpCooldown{{BeneficiaryActor: "0"}, {BeneficiaryActor: "1"}}, DisplayNameModerationMap: []types.DisplayNameModeration{{Member: "0"}, {Member: "1"}}, DisplayNameReportStakeMap: []types.DisplayNameReportStake{{ChallengeId: "0"}, {ChallengeId: "1"}}, DisplayNameAppealStakeMap: []types.DisplayNameAppealStake{{ChallengeId: "0"}, {ChallengeId: "1"}}},
			valid: false,
		}, {
			desc: "duplicated quest",
			genState: &types.GenesisState{
				QuestMap: []types.Quest{
					{
						QuestId: "0",
					},
					{
						QuestId: "0",
					},
				},
				MemberQuestProgressMap: []types.MemberQuestProgress{{MemberQuest: "0"}, {MemberQuest: "1"}}, EpochXpTrackerMap: []types.EpochXpTracker{{MemberEpoch: "0"}, {MemberEpoch: "1"}}, VoteXpRecordMap: []types.VoteXpRecord{{SeasonMemberProposal: "0"}, {SeasonMemberProposal: "1"}}, ForumXpCooldownMap: []types.ForumXpCooldown{{BeneficiaryActor: "0"}, {BeneficiaryActor: "1"}}, DisplayNameModerationMap: []types.DisplayNameModeration{{Member: "0"}, {Member: "1"}}, DisplayNameReportStakeMap: []types.DisplayNameReportStake{{ChallengeId: "0"}, {ChallengeId: "1"}}, DisplayNameAppealStakeMap: []types.DisplayNameAppealStake{{ChallengeId: "0"}, {ChallengeId: "1"}}},
			valid: false,
		}, {
			desc: "duplicated memberQuestProgress",
			genState: &types.GenesisState{
				MemberQuestProgressMap: []types.MemberQuestProgress{
					{
						MemberQuest: "0",
					},
					{
						MemberQuest: "0",
					},
				},
				EpochXpTrackerMap: []types.EpochXpTracker{{MemberEpoch: "0"}, {MemberEpoch: "1"}}, VoteXpRecordMap: []types.VoteXpRecord{{SeasonMemberProposal: "0"}, {SeasonMemberProposal: "1"}}, ForumXpCooldownMap: []types.ForumXpCooldown{{BeneficiaryActor: "0"}, {BeneficiaryActor: "1"}}, DisplayNameModerationMap: []types.DisplayNameModeration{{Member: "0"}, {Member: "1"}}, DisplayNameReportStakeMap: []types.DisplayNameReportStake{{ChallengeId: "0"}, {ChallengeId: "1"}}, DisplayNameAppealStakeMap: []types.DisplayNameAppealStake{{ChallengeId: "0"}, {ChallengeId: "1"}}},
			valid: false,
		}, {
			desc: "duplicated epochXpTracker",
			genState: &types.GenesisState{
				EpochXpTrackerMap: []types.EpochXpTracker{
					{
						MemberEpoch: "0",
					},
					{
						MemberEpoch: "0",
					},
				},
				VoteXpRecordMap: []types.VoteXpRecord{{SeasonMemberProposal: "0"}, {SeasonMemberProposal: "1"}}, ForumXpCooldownMap: []types.ForumXpCooldown{{BeneficiaryActor: "0"}, {BeneficiaryActor: "1"}}, DisplayNameModerationMap: []types.DisplayNameModeration{{Member: "0"}, {Member: "1"}}, DisplayNameReportStakeMap: []types.DisplayNameReportStake{{ChallengeId: "0"}, {ChallengeId: "1"}}, DisplayNameAppealStakeMap: []types.DisplayNameAppealStake{{ChallengeId: "0"}, {ChallengeId: "1"}}},
			valid: false,
		}, {
			desc: "duplicated voteXpRecord",
			genState: &types.GenesisState{
				VoteXpRecordMap: []types.VoteXpRecord{
					{
						SeasonMemberProposal: "0",
					},
					{
						SeasonMemberProposal: "0",
					},
				},
				ForumXpCooldownMap: []types.ForumXpCooldown{{BeneficiaryActor: "0"}, {BeneficiaryActor: "1"}}, DisplayNameModerationMap: []types.DisplayNameModeration{{Member: "0"}, {Member: "1"}}, DisplayNameReportStakeMap: []types.DisplayNameReportStake{{ChallengeId: "0"}, {ChallengeId: "1"}}, DisplayNameAppealStakeMap: []types.DisplayNameAppealStake{{ChallengeId: "0"}, {ChallengeId: "1"}}},
			valid: false,
		}, {
			desc: "duplicated forumXpCooldown",
			genState: &types.GenesisState{
				ForumXpCooldownMap: []types.ForumXpCooldown{
					{
						BeneficiaryActor: "0",
					},
					{
						BeneficiaryActor: "0",
					},
				},
				DisplayNameModerationMap: []types.DisplayNameModeration{{Member: "0"}, {Member: "1"}}, DisplayNameReportStakeMap: []types.DisplayNameReportStake{{ChallengeId: "0"}, {ChallengeId: "1"}}, DisplayNameAppealStakeMap: []types.DisplayNameAppealStake{{ChallengeId: "0"}, {ChallengeId: "1"}}},
			valid: false,
		}, {
			desc: "duplicated displayNameModeration",
			genState: &types.GenesisState{
				DisplayNameModerationMap: []types.DisplayNameModeration{
					{
						Member: "0",
					},
					{
						Member: "0",
					},
				},
				DisplayNameReportStakeMap: []types.DisplayNameReportStake{{ChallengeId: "0"}, {ChallengeId: "1"}}, DisplayNameAppealStakeMap: []types.DisplayNameAppealStake{{ChallengeId: "0"}, {ChallengeId: "1"}}},
			valid: false,
		}, {
			desc: "duplicated displayNameReportStake",
			genState: &types.GenesisState{
				DisplayNameReportStakeMap: []types.DisplayNameReportStake{
					{
						ChallengeId: "0",
					},
					{
						ChallengeId: "0",
					},
				},
				DisplayNameAppealStakeMap: []types.DisplayNameAppealStake{{ChallengeId: "0"}, {ChallengeId: "1"}}},
			valid: false,
		}, {
			desc: "duplicated displayNameAppealStake",
			genState: &types.GenesisState{
				DisplayNameAppealStakeMap: []types.DisplayNameAppealStake{
					{
						ChallengeId: "0",
					},
					{
						ChallengeId: "0",
					},
				},
			},
			valid: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			err := tc.genState.Validate()
			if tc.valid {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}

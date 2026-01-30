package types_test

import (
	"testing"

	"sparkdream/x/forum/types"

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
			desc:     "valid genesis state",
			genState: &types.GenesisState{PostMap: []types.Post{{PostId: 0}, {PostId: 1}}, CategoryMap: []types.Category{{CategoryId: 0}, {CategoryId: 1}}, TagMap: []types.Tag{{Name: "0"}, {Name: "1"}}, TagMap: []types.Tag{{Name: "0"}, {Name: "1"}}, ReservedTagMap: []types.ReservedTag{{Name: "0"}, {Name: "1"}}, ReservedTagMap: []types.ReservedTag{{Name: "0"}, {Name: "1"}}, UserRateLimitMap: []types.UserRateLimit{{UserAddress: "0"}, {UserAddress: "1"}}, UserReactionLimitMap: []types.UserReactionLimit{{UserAddress: "0"}, {UserAddress: "1"}}, SentinelActivityMap: []types.SentinelActivity{{Address: "0"}, {Address: "1"}}, HideRecordMap: []types.HideRecord{{PostId: 0}, {PostId: 1}}, ThreadLockRecordMap: []types.ThreadLockRecord{{RootId: 0}, {RootId: 1}}, ThreadMoveRecordMap: []types.ThreadMoveRecord{{RootId: 0}, {RootId: 1}}, PostFlagMap: []types.PostFlag{{PostId: 0}, {PostId: 1}}, BountyList: []types.Bounty{{Id: 0}, {Id: 1}}, BountyCount: 2, TagBudgetList: []types.TagBudget{{Id: 0}, {Id: 1}}, TagBudgetCount: 2, TagBudgetAwardList: []types.TagBudgetAward{{Id: 0}, {Id: 1}}, TagBudgetAwardCount: 2, ThreadMetadataMap: []types.ThreadMetadata{{ThreadId: 0}, {ThreadId: 1}}, ThreadFollowMap: []types.ThreadFollow{{Follower: "0"}, {Follower: "1"}}, ThreadFollowCountMap: []types.ThreadFollowCount{{ThreadId: 0}, {ThreadId: 1}}, ArchivedThreadMap: []types.ArchivedThread{{RootId: 0}, {RootId: 1}}, ArchiveMetadataMap: []types.ArchiveMetadata{{RootId: 0}, {RootId: 1}}, TagReportMap: []types.TagReport{{TagName: "0"}, {TagName: "1"}}, MemberSalvationStatusMap: []types.MemberSalvationStatus{{Address: "0"}, {Address: "1"}}, JuryParticipationMap: []types.JuryParticipation{{Juror: "0"}, {Juror: "1"}}, MemberReportMap: []types.MemberReport{{Member: "0"}, {Member: "1"}}, MemberWarningList: []types.MemberWarning{{Id: 0}, {Id: 1}}, MemberWarningCount: 2, GovActionAppealList: []types.GovActionAppeal{{Id: 0}, {Id: 1}}, GovActionAppealCount: 2}, valid: true,
		}, {
			desc: "duplicated post",
			genState: &types.GenesisState{
				PostMap: []types.Post{
					{
						PostId: 0,
					},
					{
						PostId: 0,
					},
				},
				CategoryMap: []types.Category{{CategoryId: 0}, {CategoryId: 1}}, TagMap: []types.Tag{{Name: "0"}, {Name: "1"}}, TagMap: []types.Tag{{Name: "0"}, {Name: "1"}}, ReservedTagMap: []types.ReservedTag{{Name: "0"}, {Name: "1"}}, ReservedTagMap: []types.ReservedTag{{Name: "0"}, {Name: "1"}}, UserRateLimitMap: []types.UserRateLimit{{UserAddress: "0"}, {UserAddress: "1"}}, UserReactionLimitMap: []types.UserReactionLimit{{UserAddress: "0"}, {UserAddress: "1"}}, SentinelActivityMap: []types.SentinelActivity{{Address: "0"}, {Address: "1"}}, HideRecordMap: []types.HideRecord{{PostId: 0}, {PostId: 1}}, ThreadLockRecordMap: []types.ThreadLockRecord{{RootId: 0}, {RootId: 1}}, ThreadMoveRecordMap: []types.ThreadMoveRecord{{RootId: 0}, {RootId: 1}}, PostFlagMap: []types.PostFlag{{PostId: 0}, {PostId: 1}}, BountyList: []types.Bounty{{Id: 0}, {Id: 1}}, BountyCount: 2,
				TagBudgetList: []types.TagBudget{{Id: 0}, {Id: 1}}, TagBudgetCount: 2, TagBudgetAwardList: []types.TagBudgetAward{{Id: 0}, {Id: 1}}, TagBudgetAwardCount: 2, ThreadMetadataMap: []types.ThreadMetadata{{ThreadId: 0}, {ThreadId: 1}}, ThreadFollowMap: []types.ThreadFollow{{Follower: "0"}, {Follower: "1"}}, ThreadFollowCountMap: []types.ThreadFollowCount{{ThreadId: 0}, {ThreadId: 1}}, ArchivedThreadMap: []types.ArchivedThread{{RootId: 0}, {RootId: 1}}, ArchiveMetadataMap: []types.ArchiveMetadata{{RootId: 0}, {RootId: 1}}, TagReportMap: []types.TagReport{{TagName: "0"}, {TagName: "1"}}, MemberSalvationStatusMap: []types.MemberSalvationStatus{{Address: "0"}, {Address: "1"}}, JuryParticipationMap: []types.JuryParticipation{{Juror: "0"}, {Juror: "1"}}, MemberReportMap: []types.MemberReport{{Member: "0"}, {Member: "1"}}, MemberWarningList: []types.MemberWarning{{Id: 0}, {Id: 1}}, MemberWarningCount: 2, GovActionAppealList: []types.GovActionAppeal{{Id: 0}, {Id: 1}}, GovActionAppealCount: 2}, valid: false,
		}, {
			desc: "duplicated category",
			genState: &types.GenesisState{
				CategoryMap: []types.Category{
					{
						CategoryId: 0,
					},
					{
						CategoryId: 0,
					},
				},
				TagMap: []types.Tag{{Name: "0"}, {Name: "1"}}, TagMap: []types.Tag{{Name: "0"}, {Name: "1"}}, ReservedTagMap: []types.ReservedTag{{Name: "0"}, {Name: "1"}}, ReservedTagMap: []types.ReservedTag{{Name: "0"}, {Name: "1"}}, UserRateLimitMap: []types.UserRateLimit{{UserAddress: "0"}, {UserAddress: "1"}}, UserReactionLimitMap: []types.UserReactionLimit{{UserAddress: "0"}, {UserAddress: "1"}}, SentinelActivityMap: []types.SentinelActivity{{Address: "0"}, {Address: "1"}}, HideRecordMap: []types.HideRecord{{PostId: 0}, {PostId: 1}}, ThreadLockRecordMap: []types.ThreadLockRecord{{RootId: 0}, {RootId: 1}}, ThreadMoveRecordMap: []types.ThreadMoveRecord{{RootId: 0}, {RootId: 1}}, PostFlagMap: []types.PostFlag{{PostId: 0}, {PostId: 1}}, BountyList: []types.Bounty{{Id: 0}, {Id: 1}}, BountyCount: 2,
				TagBudgetList: []types.TagBudget{{Id: 0}, {Id: 1}}, TagBudgetCount: 2, TagBudgetAwardList: []types.TagBudgetAward{{Id: 0}, {Id: 1}}, TagBudgetAwardCount: 2, ThreadMetadataMap: []types.ThreadMetadata{{ThreadId: 0}, {ThreadId: 1}}, ThreadFollowMap: []types.ThreadFollow{{Follower: "0"}, {Follower: "1"}}, ThreadFollowCountMap: []types.ThreadFollowCount{{ThreadId: 0}, {ThreadId: 1}}, ArchivedThreadMap: []types.ArchivedThread{{RootId: 0}, {RootId: 1}}, ArchiveMetadataMap: []types.ArchiveMetadata{{RootId: 0}, {RootId: 1}}, TagReportMap: []types.TagReport{{TagName: "0"}, {TagName: "1"}}, MemberSalvationStatusMap: []types.MemberSalvationStatus{{Address: "0"}, {Address: "1"}}, JuryParticipationMap: []types.JuryParticipation{{Juror: "0"}, {Juror: "1"}}, MemberReportMap: []types.MemberReport{{Member: "0"}, {Member: "1"}}, MemberWarningList: []types.MemberWarning{{Id: 0}, {Id: 1}}, MemberWarningCount: 2, GovActionAppealList: []types.GovActionAppeal{{Id: 0}, {Id: 1}}, GovActionAppealCount: 2}, valid: false,
		}, {
			desc: "duplicated tag",
			genState: &types.GenesisState{
				TagMap: []types.Tag{
					{
						Name: "0",
					},
					{
						Name: "0",
					},
				},
				TagMap: []types.Tag{{Name: "0"}, {Name: "1"}}, ReservedTagMap: []types.ReservedTag{{Name: "0"}, {Name: "1"}}, ReservedTagMap: []types.ReservedTag{{Name: "0"}, {Name: "1"}}, UserRateLimitMap: []types.UserRateLimit{{UserAddress: "0"}, {UserAddress: "1"}}, UserReactionLimitMap: []types.UserReactionLimit{{UserAddress: "0"}, {UserAddress: "1"}}, SentinelActivityMap: []types.SentinelActivity{{Address: "0"}, {Address: "1"}}, HideRecordMap: []types.HideRecord{{PostId: 0}, {PostId: 1}}, ThreadLockRecordMap: []types.ThreadLockRecord{{RootId: 0}, {RootId: 1}}, ThreadMoveRecordMap: []types.ThreadMoveRecord{{RootId: 0}, {RootId: 1}}, PostFlagMap: []types.PostFlag{{PostId: 0}, {PostId: 1}}, BountyList: []types.Bounty{{Id: 0}, {Id: 1}}, BountyCount: 2,
				TagBudgetList: []types.TagBudget{{Id: 0}, {Id: 1}}, TagBudgetCount: 2, TagBudgetAwardList: []types.TagBudgetAward{{Id: 0}, {Id: 1}}, TagBudgetAwardCount: 2, ThreadMetadataMap: []types.ThreadMetadata{{ThreadId: 0}, {ThreadId: 1}}, ThreadFollowMap: []types.ThreadFollow{{Follower: "0"}, {Follower: "1"}}, ThreadFollowCountMap: []types.ThreadFollowCount{{ThreadId: 0}, {ThreadId: 1}}, ArchivedThreadMap: []types.ArchivedThread{{RootId: 0}, {RootId: 1}}, ArchiveMetadataMap: []types.ArchiveMetadata{{RootId: 0}, {RootId: 1}}, TagReportMap: []types.TagReport{{TagName: "0"}, {TagName: "1"}}, MemberSalvationStatusMap: []types.MemberSalvationStatus{{Address: "0"}, {Address: "1"}}, JuryParticipationMap: []types.JuryParticipation{{Juror: "0"}, {Juror: "1"}}, MemberReportMap: []types.MemberReport{{Member: "0"}, {Member: "1"}}, MemberWarningList: []types.MemberWarning{{Id: 0}, {Id: 1}}, MemberWarningCount: 2, GovActionAppealList: []types.GovActionAppeal{{Id: 0}, {Id: 1}}, GovActionAppealCount: 2}, valid: false,
		}, {
			desc: "duplicated tag",
			genState: &types.GenesisState{
				TagMap: []types.Tag{
					{
						Name: "0",
					},
					{
						Name: "0",
					},
				},
				ReservedTagMap: []types.ReservedTag{{Name: "0"}, {Name: "1"}}, ReservedTagMap: []types.ReservedTag{{Name: "0"}, {Name: "1"}}, UserRateLimitMap: []types.UserRateLimit{{UserAddress: "0"}, {UserAddress: "1"}}, UserReactionLimitMap: []types.UserReactionLimit{{UserAddress: "0"}, {UserAddress: "1"}}, SentinelActivityMap: []types.SentinelActivity{{Address: "0"}, {Address: "1"}}, HideRecordMap: []types.HideRecord{{PostId: 0}, {PostId: 1}}, ThreadLockRecordMap: []types.ThreadLockRecord{{RootId: 0}, {RootId: 1}}, ThreadMoveRecordMap: []types.ThreadMoveRecord{{RootId: 0}, {RootId: 1}}, PostFlagMap: []types.PostFlag{{PostId: 0}, {PostId: 1}}, BountyList: []types.Bounty{{Id: 0}, {Id: 1}}, BountyCount: 2,
				TagBudgetList: []types.TagBudget{{Id: 0}, {Id: 1}}, TagBudgetCount: 2, TagBudgetAwardList: []types.TagBudgetAward{{Id: 0}, {Id: 1}}, TagBudgetAwardCount: 2, ThreadMetadataMap: []types.ThreadMetadata{{ThreadId: 0}, {ThreadId: 1}}, ThreadFollowMap: []types.ThreadFollow{{Follower: "0"}, {Follower: "1"}}, ThreadFollowCountMap: []types.ThreadFollowCount{{ThreadId: 0}, {ThreadId: 1}}, ArchivedThreadMap: []types.ArchivedThread{{RootId: 0}, {RootId: 1}}, ArchiveMetadataMap: []types.ArchiveMetadata{{RootId: 0}, {RootId: 1}}, TagReportMap: []types.TagReport{{TagName: "0"}, {TagName: "1"}}, MemberSalvationStatusMap: []types.MemberSalvationStatus{{Address: "0"}, {Address: "1"}}, JuryParticipationMap: []types.JuryParticipation{{Juror: "0"}, {Juror: "1"}}, MemberReportMap: []types.MemberReport{{Member: "0"}, {Member: "1"}}, MemberWarningList: []types.MemberWarning{{Id: 0}, {Id: 1}}, MemberWarningCount: 2, GovActionAppealList: []types.GovActionAppeal{{Id: 0}, {Id: 1}}, GovActionAppealCount: 2}, valid: false,
		}, {
			desc: "duplicated reservedTag",
			genState: &types.GenesisState{
				ReservedTagMap: []types.ReservedTag{
					{
						Name: "0",
					},
					{
						Name: "0",
					},
				},
				ReservedTagMap: []types.ReservedTag{{Name: "0"}, {Name: "1"}}, UserRateLimitMap: []types.UserRateLimit{{UserAddress: "0"}, {UserAddress: "1"}}, UserReactionLimitMap: []types.UserReactionLimit{{UserAddress: "0"}, {UserAddress: "1"}}, SentinelActivityMap: []types.SentinelActivity{{Address: "0"}, {Address: "1"}}, HideRecordMap: []types.HideRecord{{PostId: 0}, {PostId: 1}}, ThreadLockRecordMap: []types.ThreadLockRecord{{RootId: 0}, {RootId: 1}}, ThreadMoveRecordMap: []types.ThreadMoveRecord{{RootId: 0}, {RootId: 1}}, PostFlagMap: []types.PostFlag{{PostId: 0}, {PostId: 1}}, BountyList: []types.Bounty{{Id: 0}, {Id: 1}}, BountyCount: 2,
				TagBudgetList: []types.TagBudget{{Id: 0}, {Id: 1}}, TagBudgetCount: 2, TagBudgetAwardList: []types.TagBudgetAward{{Id: 0}, {Id: 1}}, TagBudgetAwardCount: 2, ThreadMetadataMap: []types.ThreadMetadata{{ThreadId: 0}, {ThreadId: 1}}, ThreadFollowMap: []types.ThreadFollow{{Follower: "0"}, {Follower: "1"}}, ThreadFollowCountMap: []types.ThreadFollowCount{{ThreadId: 0}, {ThreadId: 1}}, ArchivedThreadMap: []types.ArchivedThread{{RootId: 0}, {RootId: 1}}, ArchiveMetadataMap: []types.ArchiveMetadata{{RootId: 0}, {RootId: 1}}, TagReportMap: []types.TagReport{{TagName: "0"}, {TagName: "1"}}, MemberSalvationStatusMap: []types.MemberSalvationStatus{{Address: "0"}, {Address: "1"}}, JuryParticipationMap: []types.JuryParticipation{{Juror: "0"}, {Juror: "1"}}, MemberReportMap: []types.MemberReport{{Member: "0"}, {Member: "1"}}, MemberWarningList: []types.MemberWarning{{Id: 0}, {Id: 1}}, MemberWarningCount: 2, GovActionAppealList: []types.GovActionAppeal{{Id: 0}, {Id: 1}}, GovActionAppealCount: 2}, valid: false,
		}, {
			desc: "duplicated reservedTag",
			genState: &types.GenesisState{
				ReservedTagMap: []types.ReservedTag{
					{
						Name: "0",
					},
					{
						Name: "0",
					},
				},
				UserRateLimitMap: []types.UserRateLimit{{UserAddress: "0"}, {UserAddress: "1"}}, UserReactionLimitMap: []types.UserReactionLimit{{UserAddress: "0"}, {UserAddress: "1"}}, SentinelActivityMap: []types.SentinelActivity{{Address: "0"}, {Address: "1"}}, HideRecordMap: []types.HideRecord{{PostId: 0}, {PostId: 1}}, ThreadLockRecordMap: []types.ThreadLockRecord{{RootId: 0}, {RootId: 1}}, ThreadMoveRecordMap: []types.ThreadMoveRecord{{RootId: 0}, {RootId: 1}}, PostFlagMap: []types.PostFlag{{PostId: 0}, {PostId: 1}}, BountyList: []types.Bounty{{Id: 0}, {Id: 1}}, BountyCount: 2,
				TagBudgetList: []types.TagBudget{{Id: 0}, {Id: 1}}, TagBudgetCount: 2, TagBudgetAwardList: []types.TagBudgetAward{{Id: 0}, {Id: 1}}, TagBudgetAwardCount: 2, ThreadMetadataMap: []types.ThreadMetadata{{ThreadId: 0}, {ThreadId: 1}}, ThreadFollowMap: []types.ThreadFollow{{Follower: "0"}, {Follower: "1"}}, ThreadFollowCountMap: []types.ThreadFollowCount{{ThreadId: 0}, {ThreadId: 1}}, ArchivedThreadMap: []types.ArchivedThread{{RootId: 0}, {RootId: 1}}, ArchiveMetadataMap: []types.ArchiveMetadata{{RootId: 0}, {RootId: 1}}, TagReportMap: []types.TagReport{{TagName: "0"}, {TagName: "1"}}, MemberSalvationStatusMap: []types.MemberSalvationStatus{{Address: "0"}, {Address: "1"}}, JuryParticipationMap: []types.JuryParticipation{{Juror: "0"}, {Juror: "1"}}, MemberReportMap: []types.MemberReport{{Member: "0"}, {Member: "1"}}, MemberWarningList: []types.MemberWarning{{Id: 0}, {Id: 1}}, MemberWarningCount: 2, GovActionAppealList: []types.GovActionAppeal{{Id: 0}, {Id: 1}}, GovActionAppealCount: 2}, valid: false,
		}, {
			desc: "duplicated userRateLimit",
			genState: &types.GenesisState{
				UserRateLimitMap: []types.UserRateLimit{
					{
						UserAddress: "0",
					},
					{
						UserAddress: "0",
					},
				},
				UserReactionLimitMap: []types.UserReactionLimit{{UserAddress: "0"}, {UserAddress: "1"}}, SentinelActivityMap: []types.SentinelActivity{{Address: "0"}, {Address: "1"}}, HideRecordMap: []types.HideRecord{{PostId: 0}, {PostId: 1}}, ThreadLockRecordMap: []types.ThreadLockRecord{{RootId: 0}, {RootId: 1}}, ThreadMoveRecordMap: []types.ThreadMoveRecord{{RootId: 0}, {RootId: 1}}, PostFlagMap: []types.PostFlag{{PostId: 0}, {PostId: 1}}, BountyList: []types.Bounty{{Id: 0}, {Id: 1}}, BountyCount: 2,
				TagBudgetList: []types.TagBudget{{Id: 0}, {Id: 1}}, TagBudgetCount: 2, TagBudgetAwardList: []types.TagBudgetAward{{Id: 0}, {Id: 1}}, TagBudgetAwardCount: 2, ThreadMetadataMap: []types.ThreadMetadata{{ThreadId: 0}, {ThreadId: 1}}, ThreadFollowMap: []types.ThreadFollow{{Follower: "0"}, {Follower: "1"}}, ThreadFollowCountMap: []types.ThreadFollowCount{{ThreadId: 0}, {ThreadId: 1}}, ArchivedThreadMap: []types.ArchivedThread{{RootId: 0}, {RootId: 1}}, ArchiveMetadataMap: []types.ArchiveMetadata{{RootId: 0}, {RootId: 1}}, TagReportMap: []types.TagReport{{TagName: "0"}, {TagName: "1"}}, MemberSalvationStatusMap: []types.MemberSalvationStatus{{Address: "0"}, {Address: "1"}}, JuryParticipationMap: []types.JuryParticipation{{Juror: "0"}, {Juror: "1"}}, MemberReportMap: []types.MemberReport{{Member: "0"}, {Member: "1"}}, MemberWarningList: []types.MemberWarning{{Id: 0}, {Id: 1}}, MemberWarningCount: 2, GovActionAppealList: []types.GovActionAppeal{{Id: 0}, {Id: 1}}, GovActionAppealCount: 2}, valid: false,
		}, {
			desc: "duplicated userReactionLimit",
			genState: &types.GenesisState{
				UserReactionLimitMap: []types.UserReactionLimit{
					{
						UserAddress: "0",
					},
					{
						UserAddress: "0",
					},
				},
				SentinelActivityMap: []types.SentinelActivity{{Address: "0"}, {Address: "1"}}, HideRecordMap: []types.HideRecord{{PostId: 0}, {PostId: 1}}, ThreadLockRecordMap: []types.ThreadLockRecord{{RootId: 0}, {RootId: 1}}, ThreadMoveRecordMap: []types.ThreadMoveRecord{{RootId: 0}, {RootId: 1}}, PostFlagMap: []types.PostFlag{{PostId: 0}, {PostId: 1}}, BountyList: []types.Bounty{{Id: 0}, {Id: 1}}, BountyCount: 2,
				TagBudgetList: []types.TagBudget{{Id: 0}, {Id: 1}}, TagBudgetCount: 2, TagBudgetAwardList: []types.TagBudgetAward{{Id: 0}, {Id: 1}}, TagBudgetAwardCount: 2, ThreadMetadataMap: []types.ThreadMetadata{{ThreadId: 0}, {ThreadId: 1}}, ThreadFollowMap: []types.ThreadFollow{{Follower: "0"}, {Follower: "1"}}, ThreadFollowCountMap: []types.ThreadFollowCount{{ThreadId: 0}, {ThreadId: 1}}, ArchivedThreadMap: []types.ArchivedThread{{RootId: 0}, {RootId: 1}}, ArchiveMetadataMap: []types.ArchiveMetadata{{RootId: 0}, {RootId: 1}}, TagReportMap: []types.TagReport{{TagName: "0"}, {TagName: "1"}}, MemberSalvationStatusMap: []types.MemberSalvationStatus{{Address: "0"}, {Address: "1"}}, JuryParticipationMap: []types.JuryParticipation{{Juror: "0"}, {Juror: "1"}}, MemberReportMap: []types.MemberReport{{Member: "0"}, {Member: "1"}}, MemberWarningList: []types.MemberWarning{{Id: 0}, {Id: 1}}, MemberWarningCount: 2, GovActionAppealList: []types.GovActionAppeal{{Id: 0}, {Id: 1}}, GovActionAppealCount: 2}, valid: false,
		}, {
			desc: "duplicated sentinelActivity",
			genState: &types.GenesisState{
				SentinelActivityMap: []types.SentinelActivity{
					{
						Address: "0",
					},
					{
						Address: "0",
					},
				},
				HideRecordMap: []types.HideRecord{{PostId: 0}, {PostId: 1}}, ThreadLockRecordMap: []types.ThreadLockRecord{{RootId: 0}, {RootId: 1}}, ThreadMoveRecordMap: []types.ThreadMoveRecord{{RootId: 0}, {RootId: 1}}, PostFlagMap: []types.PostFlag{{PostId: 0}, {PostId: 1}}, BountyList: []types.Bounty{{Id: 0}, {Id: 1}}, BountyCount: 2,
				TagBudgetList: []types.TagBudget{{Id: 0}, {Id: 1}}, TagBudgetCount: 2, TagBudgetAwardList: []types.TagBudgetAward{{Id: 0}, {Id: 1}}, TagBudgetAwardCount: 2, ThreadMetadataMap: []types.ThreadMetadata{{ThreadId: 0}, {ThreadId: 1}}, ThreadFollowMap: []types.ThreadFollow{{Follower: "0"}, {Follower: "1"}}, ThreadFollowCountMap: []types.ThreadFollowCount{{ThreadId: 0}, {ThreadId: 1}}, ArchivedThreadMap: []types.ArchivedThread{{RootId: 0}, {RootId: 1}}, ArchiveMetadataMap: []types.ArchiveMetadata{{RootId: 0}, {RootId: 1}}, TagReportMap: []types.TagReport{{TagName: "0"}, {TagName: "1"}}, MemberSalvationStatusMap: []types.MemberSalvationStatus{{Address: "0"}, {Address: "1"}}, JuryParticipationMap: []types.JuryParticipation{{Juror: "0"}, {Juror: "1"}}, MemberReportMap: []types.MemberReport{{Member: "0"}, {Member: "1"}}, MemberWarningList: []types.MemberWarning{{Id: 0}, {Id: 1}}, MemberWarningCount: 2, GovActionAppealList: []types.GovActionAppeal{{Id: 0}, {Id: 1}}, GovActionAppealCount: 2}, valid: false,
		}, {
			desc: "duplicated hideRecord",
			genState: &types.GenesisState{
				HideRecordMap: []types.HideRecord{
					{
						PostId: 0,
					},
					{
						PostId: 0,
					},
				},
				ThreadLockRecordMap: []types.ThreadLockRecord{{RootId: 0}, {RootId: 1}}, ThreadMoveRecordMap: []types.ThreadMoveRecord{{RootId: 0}, {RootId: 1}}, PostFlagMap: []types.PostFlag{{PostId: 0}, {PostId: 1}}, BountyList: []types.Bounty{{Id: 0}, {Id: 1}}, BountyCount: 2,
				TagBudgetList: []types.TagBudget{{Id: 0}, {Id: 1}}, TagBudgetCount: 2, TagBudgetAwardList: []types.TagBudgetAward{{Id: 0}, {Id: 1}}, TagBudgetAwardCount: 2, ThreadMetadataMap: []types.ThreadMetadata{{ThreadId: 0}, {ThreadId: 1}}, ThreadFollowMap: []types.ThreadFollow{{Follower: "0"}, {Follower: "1"}}, ThreadFollowCountMap: []types.ThreadFollowCount{{ThreadId: 0}, {ThreadId: 1}}, ArchivedThreadMap: []types.ArchivedThread{{RootId: 0}, {RootId: 1}}, ArchiveMetadataMap: []types.ArchiveMetadata{{RootId: 0}, {RootId: 1}}, TagReportMap: []types.TagReport{{TagName: "0"}, {TagName: "1"}}, MemberSalvationStatusMap: []types.MemberSalvationStatus{{Address: "0"}, {Address: "1"}}, JuryParticipationMap: []types.JuryParticipation{{Juror: "0"}, {Juror: "1"}}, MemberReportMap: []types.MemberReport{{Member: "0"}, {Member: "1"}}, MemberWarningList: []types.MemberWarning{{Id: 0}, {Id: 1}}, MemberWarningCount: 2, GovActionAppealList: []types.GovActionAppeal{{Id: 0}, {Id: 1}}, GovActionAppealCount: 2}, valid: false,
		}, {
			desc: "duplicated threadLockRecord",
			genState: &types.GenesisState{
				ThreadLockRecordMap: []types.ThreadLockRecord{
					{
						RootId: 0,
					},
					{
						RootId: 0,
					},
				},
				ThreadMoveRecordMap: []types.ThreadMoveRecord{{RootId: 0}, {RootId: 1}}, PostFlagMap: []types.PostFlag{{PostId: 0}, {PostId: 1}}, BountyList: []types.Bounty{{Id: 0}, {Id: 1}}, BountyCount: 2,
				TagBudgetList: []types.TagBudget{{Id: 0}, {Id: 1}}, TagBudgetCount: 2, TagBudgetAwardList: []types.TagBudgetAward{{Id: 0}, {Id: 1}}, TagBudgetAwardCount: 2, ThreadMetadataMap: []types.ThreadMetadata{{ThreadId: 0}, {ThreadId: 1}}, ThreadFollowMap: []types.ThreadFollow{{Follower: "0"}, {Follower: "1"}}, ThreadFollowCountMap: []types.ThreadFollowCount{{ThreadId: 0}, {ThreadId: 1}}, ArchivedThreadMap: []types.ArchivedThread{{RootId: 0}, {RootId: 1}}, ArchiveMetadataMap: []types.ArchiveMetadata{{RootId: 0}, {RootId: 1}}, TagReportMap: []types.TagReport{{TagName: "0"}, {TagName: "1"}}, MemberSalvationStatusMap: []types.MemberSalvationStatus{{Address: "0"}, {Address: "1"}}, JuryParticipationMap: []types.JuryParticipation{{Juror: "0"}, {Juror: "1"}}, MemberReportMap: []types.MemberReport{{Member: "0"}, {Member: "1"}}, MemberWarningList: []types.MemberWarning{{Id: 0}, {Id: 1}}, MemberWarningCount: 2, GovActionAppealList: []types.GovActionAppeal{{Id: 0}, {Id: 1}}, GovActionAppealCount: 2}, valid: false,
		}, {
			desc: "duplicated threadMoveRecord",
			genState: &types.GenesisState{
				ThreadMoveRecordMap: []types.ThreadMoveRecord{
					{
						RootId: 0,
					},
					{
						RootId: 0,
					},
				},
				PostFlagMap: []types.PostFlag{{PostId: 0}, {PostId: 1}}, BountyList: []types.Bounty{{Id: 0}, {Id: 1}}, BountyCount: 2,
				TagBudgetList: []types.TagBudget{{Id: 0}, {Id: 1}}, TagBudgetCount: 2, TagBudgetAwardList: []types.TagBudgetAward{{Id: 0}, {Id: 1}}, TagBudgetAwardCount: 2, ThreadMetadataMap: []types.ThreadMetadata{{ThreadId: 0}, {ThreadId: 1}}, ThreadFollowMap: []types.ThreadFollow{{Follower: "0"}, {Follower: "1"}}, ThreadFollowCountMap: []types.ThreadFollowCount{{ThreadId: 0}, {ThreadId: 1}}, ArchivedThreadMap: []types.ArchivedThread{{RootId: 0}, {RootId: 1}}, ArchiveMetadataMap: []types.ArchiveMetadata{{RootId: 0}, {RootId: 1}}, TagReportMap: []types.TagReport{{TagName: "0"}, {TagName: "1"}}, MemberSalvationStatusMap: []types.MemberSalvationStatus{{Address: "0"}, {Address: "1"}}, JuryParticipationMap: []types.JuryParticipation{{Juror: "0"}, {Juror: "1"}}, MemberReportMap: []types.MemberReport{{Member: "0"}, {Member: "1"}}, MemberWarningList: []types.MemberWarning{{Id: 0}, {Id: 1}}, MemberWarningCount: 2, GovActionAppealList: []types.GovActionAppeal{{Id: 0}, {Id: 1}}, GovActionAppealCount: 2}, valid: false,
		}, {
			desc: "duplicated postFlag",
			genState: &types.GenesisState{
				PostFlagMap: []types.PostFlag{
					{
						PostId: 0,
					},
					{
						PostId: 0,
					},
				},
				BountyList: []types.Bounty{{Id: 0}, {Id: 1}}, BountyCount: 2,
				TagBudgetList: []types.TagBudget{{Id: 0}, {Id: 1}}, TagBudgetCount: 2, TagBudgetAwardList: []types.TagBudgetAward{{Id: 0}, {Id: 1}}, TagBudgetAwardCount: 2, ThreadMetadataMap: []types.ThreadMetadata{{ThreadId: 0}, {ThreadId: 1}}, ThreadFollowMap: []types.ThreadFollow{{Follower: "0"}, {Follower: "1"}}, ThreadFollowCountMap: []types.ThreadFollowCount{{ThreadId: 0}, {ThreadId: 1}}, ArchivedThreadMap: []types.ArchivedThread{{RootId: 0}, {RootId: 1}}, ArchiveMetadataMap: []types.ArchiveMetadata{{RootId: 0}, {RootId: 1}}, TagReportMap: []types.TagReport{{TagName: "0"}, {TagName: "1"}}, MemberSalvationStatusMap: []types.MemberSalvationStatus{{Address: "0"}, {Address: "1"}}, JuryParticipationMap: []types.JuryParticipation{{Juror: "0"}, {Juror: "1"}}, MemberReportMap: []types.MemberReport{{Member: "0"}, {Member: "1"}}, MemberWarningList: []types.MemberWarning{{Id: 0}, {Id: 1}}, MemberWarningCount: 2, GovActionAppealList: []types.GovActionAppeal{{Id: 0}, {Id: 1}}, GovActionAppealCount: 2}, valid: false,
		}, {
			desc: "duplicated bounty",
			genState: &types.GenesisState{
				BountyList: []types.Bounty{
					{
						Id: 0,
					},
					{
						Id: 0,
					},
				},
				TagBudgetList: []types.TagBudget{{Id: 0}, {Id: 1}}, TagBudgetCount: 2,
				TagBudgetAwardList: []types.TagBudgetAward{{Id: 0}, {Id: 1}}, TagBudgetAwardCount: 2, ThreadMetadataMap: []types.ThreadMetadata{{ThreadId: 0}, {ThreadId: 1}}, ThreadFollowMap: []types.ThreadFollow{{Follower: "0"}, {Follower: "1"}}, ThreadFollowCountMap: []types.ThreadFollowCount{{ThreadId: 0}, {ThreadId: 1}}, ArchivedThreadMap: []types.ArchivedThread{{RootId: 0}, {RootId: 1}}, ArchiveMetadataMap: []types.ArchiveMetadata{{RootId: 0}, {RootId: 1}}, TagReportMap: []types.TagReport{{TagName: "0"}, {TagName: "1"}}, MemberSalvationStatusMap: []types.MemberSalvationStatus{{Address: "0"}, {Address: "1"}}, JuryParticipationMap: []types.JuryParticipation{{Juror: "0"}, {Juror: "1"}}, MemberReportMap: []types.MemberReport{{Member: "0"}, {Member: "1"}}, MemberWarningList: []types.MemberWarning{{Id: 0}, {Id: 1}}, MemberWarningCount: 2, GovActionAppealList: []types.GovActionAppeal{{Id: 0}, {Id: 1}}, GovActionAppealCount: 2}, valid: false,
		}, {
			desc: "invalid bounty count",
			genState: &types.GenesisState{
				BountyList: []types.Bounty{
					{
						Id: 1,
					},
				},
				BountyCount:   0,
				TagBudgetList: []types.TagBudget{{Id: 0}, {Id: 1}}, TagBudgetCount: 2,
				TagBudgetAwardList: []types.TagBudgetAward{{Id: 0}, {Id: 1}}, TagBudgetAwardCount: 2, ThreadMetadataMap: []types.ThreadMetadata{{ThreadId: 0}, {ThreadId: 1}}, ThreadFollowMap: []types.ThreadFollow{{Follower: "0"}, {Follower: "1"}}, ThreadFollowCountMap: []types.ThreadFollowCount{{ThreadId: 0}, {ThreadId: 1}}, ArchivedThreadMap: []types.ArchivedThread{{RootId: 0}, {RootId: 1}}, ArchiveMetadataMap: []types.ArchiveMetadata{{RootId: 0}, {RootId: 1}}, TagReportMap: []types.TagReport{{TagName: "0"}, {TagName: "1"}}, MemberSalvationStatusMap: []types.MemberSalvationStatus{{Address: "0"}, {Address: "1"}}, JuryParticipationMap: []types.JuryParticipation{{Juror: "0"}, {Juror: "1"}}, MemberReportMap: []types.MemberReport{{Member: "0"}, {Member: "1"}}, MemberWarningList: []types.MemberWarning{{Id: 0}, {Id: 1}}, MemberWarningCount: 2, GovActionAppealList: []types.GovActionAppeal{{Id: 0}, {Id: 1}}, GovActionAppealCount: 2}, valid: false,
		}, {
			desc: "duplicated tagBudget",
			genState: &types.GenesisState{
				TagBudgetList: []types.TagBudget{
					{
						Id: 0,
					},
					{
						Id: 0,
					},
				},
				TagBudgetAwardList: []types.TagBudgetAward{{Id: 0}, {Id: 1}}, TagBudgetAwardCount: 2,
				ThreadMetadataMap: []types.ThreadMetadata{{ThreadId: 0}, {ThreadId: 1}}, ThreadFollowMap: []types.ThreadFollow{{Follower: "0"}, {Follower: "1"}}, ThreadFollowCountMap: []types.ThreadFollowCount{{ThreadId: 0}, {ThreadId: 1}}, ArchivedThreadMap: []types.ArchivedThread{{RootId: 0}, {RootId: 1}}, ArchiveMetadataMap: []types.ArchiveMetadata{{RootId: 0}, {RootId: 1}}, TagReportMap: []types.TagReport{{TagName: "0"}, {TagName: "1"}}, MemberSalvationStatusMap: []types.MemberSalvationStatus{{Address: "0"}, {Address: "1"}}, JuryParticipationMap: []types.JuryParticipation{{Juror: "0"}, {Juror: "1"}}, MemberReportMap: []types.MemberReport{{Member: "0"}, {Member: "1"}}, MemberWarningList: []types.MemberWarning{{Id: 0}, {Id: 1}}, MemberWarningCount: 2, GovActionAppealList: []types.GovActionAppeal{{Id: 0}, {Id: 1}}, GovActionAppealCount: 2}, valid: false,
		}, {
			desc: "invalid tagBudget count",
			genState: &types.GenesisState{
				TagBudgetList: []types.TagBudget{
					{
						Id: 1,
					},
				},
				TagBudgetCount:     0,
				TagBudgetAwardList: []types.TagBudgetAward{{Id: 0}, {Id: 1}}, TagBudgetAwardCount: 2,
				ThreadMetadataMap: []types.ThreadMetadata{{ThreadId: 0}, {ThreadId: 1}}, ThreadFollowMap: []types.ThreadFollow{{Follower: "0"}, {Follower: "1"}}, ThreadFollowCountMap: []types.ThreadFollowCount{{ThreadId: 0}, {ThreadId: 1}}, ArchivedThreadMap: []types.ArchivedThread{{RootId: 0}, {RootId: 1}}, ArchiveMetadataMap: []types.ArchiveMetadata{{RootId: 0}, {RootId: 1}}, TagReportMap: []types.TagReport{{TagName: "0"}, {TagName: "1"}}, MemberSalvationStatusMap: []types.MemberSalvationStatus{{Address: "0"}, {Address: "1"}}, JuryParticipationMap: []types.JuryParticipation{{Juror: "0"}, {Juror: "1"}}, MemberReportMap: []types.MemberReport{{Member: "0"}, {Member: "1"}}, MemberWarningList: []types.MemberWarning{{Id: 0}, {Id: 1}}, MemberWarningCount: 2, GovActionAppealList: []types.GovActionAppeal{{Id: 0}, {Id: 1}}, GovActionAppealCount: 2}, valid: false,
		}, {
			desc: "duplicated tagBudgetAward",
			genState: &types.GenesisState{
				TagBudgetAwardList: []types.TagBudgetAward{
					{
						Id: 0,
					},
					{
						Id: 0,
					},
				},
				ThreadMetadataMap: []types.ThreadMetadata{{ThreadId: 0}, {ThreadId: 1}}, ThreadFollowMap: []types.ThreadFollow{{Follower: "0"}, {Follower: "1"}}, ThreadFollowCountMap: []types.ThreadFollowCount{{ThreadId: 0}, {ThreadId: 1}}, ArchivedThreadMap: []types.ArchivedThread{{RootId: 0}, {RootId: 1}}, ArchiveMetadataMap: []types.ArchiveMetadata{{RootId: 0}, {RootId: 1}}, TagReportMap: []types.TagReport{{TagName: "0"}, {TagName: "1"}}, MemberSalvationStatusMap: []types.MemberSalvationStatus{{Address: "0"}, {Address: "1"}}, JuryParticipationMap: []types.JuryParticipation{{Juror: "0"}, {Juror: "1"}}, MemberReportMap: []types.MemberReport{{Member: "0"}, {Member: "1"}}, MemberWarningList: []types.MemberWarning{{Id: 0}, {Id: 1}}, MemberWarningCount: 2,
				GovActionAppealList: []types.GovActionAppeal{{Id: 0}, {Id: 1}}, GovActionAppealCount: 2}, valid: false,
		}, {
			desc: "invalid tagBudgetAward count",
			genState: &types.GenesisState{
				TagBudgetAwardList: []types.TagBudgetAward{
					{
						Id: 1,
					},
				},
				TagBudgetAwardCount: 0,
				ThreadMetadataMap:   []types.ThreadMetadata{{ThreadId: 0}, {ThreadId: 1}}, ThreadFollowMap: []types.ThreadFollow{{Follower: "0"}, {Follower: "1"}}, ThreadFollowCountMap: []types.ThreadFollowCount{{ThreadId: 0}, {ThreadId: 1}}, ArchivedThreadMap: []types.ArchivedThread{{RootId: 0}, {RootId: 1}}, ArchiveMetadataMap: []types.ArchiveMetadata{{RootId: 0}, {RootId: 1}}, TagReportMap: []types.TagReport{{TagName: "0"}, {TagName: "1"}}, MemberSalvationStatusMap: []types.MemberSalvationStatus{{Address: "0"}, {Address: "1"}}, JuryParticipationMap: []types.JuryParticipation{{Juror: "0"}, {Juror: "1"}}, MemberReportMap: []types.MemberReport{{Member: "0"}, {Member: "1"}}, MemberWarningList: []types.MemberWarning{{Id: 0}, {Id: 1}}, MemberWarningCount: 2,
				GovActionAppealList: []types.GovActionAppeal{{Id: 0}, {Id: 1}}, GovActionAppealCount: 2}, valid: false,
		}, {
			desc: "duplicated threadMetadata",
			genState: &types.GenesisState{
				ThreadMetadataMap: []types.ThreadMetadata{
					{
						ThreadId: 0,
					},
					{
						ThreadId: 0,
					},
				},
				ThreadFollowMap: []types.ThreadFollow{{Follower: "0"}, {Follower: "1"}}, ThreadFollowCountMap: []types.ThreadFollowCount{{ThreadId: 0}, {ThreadId: 1}}, ArchivedThreadMap: []types.ArchivedThread{{RootId: 0}, {RootId: 1}}, ArchiveMetadataMap: []types.ArchiveMetadata{{RootId: 0}, {RootId: 1}}, TagReportMap: []types.TagReport{{TagName: "0"}, {TagName: "1"}}, MemberSalvationStatusMap: []types.MemberSalvationStatus{{Address: "0"}, {Address: "1"}}, JuryParticipationMap: []types.JuryParticipation{{Juror: "0"}, {Juror: "1"}}, MemberReportMap: []types.MemberReport{{Member: "0"}, {Member: "1"}}, MemberWarningList: []types.MemberWarning{{Id: 0}, {Id: 1}}, MemberWarningCount: 2,
				GovActionAppealList: []types.GovActionAppeal{{Id: 0}, {Id: 1}}, GovActionAppealCount: 2}, valid: false,
		}, {
			desc: "duplicated threadFollow",
			genState: &types.GenesisState{
				ThreadFollowMap: []types.ThreadFollow{
					{
						Follower: "0",
					},
					{
						Follower: "0",
					},
				},
				ThreadFollowCountMap: []types.ThreadFollowCount{{ThreadId: 0}, {ThreadId: 1}}, ArchivedThreadMap: []types.ArchivedThread{{RootId: 0}, {RootId: 1}}, ArchiveMetadataMap: []types.ArchiveMetadata{{RootId: 0}, {RootId: 1}}, TagReportMap: []types.TagReport{{TagName: "0"}, {TagName: "1"}}, MemberSalvationStatusMap: []types.MemberSalvationStatus{{Address: "0"}, {Address: "1"}}, JuryParticipationMap: []types.JuryParticipation{{Juror: "0"}, {Juror: "1"}}, MemberReportMap: []types.MemberReport{{Member: "0"}, {Member: "1"}}, MemberWarningList: []types.MemberWarning{{Id: 0}, {Id: 1}}, MemberWarningCount: 2,
				GovActionAppealList: []types.GovActionAppeal{{Id: 0}, {Id: 1}}, GovActionAppealCount: 2}, valid: false,
		}, {
			desc: "duplicated threadFollowCount",
			genState: &types.GenesisState{
				ThreadFollowCountMap: []types.ThreadFollowCount{
					{
						ThreadId: 0,
					},
					{
						ThreadId: 0,
					},
				},
				ArchivedThreadMap: []types.ArchivedThread{{RootId: 0}, {RootId: 1}}, ArchiveMetadataMap: []types.ArchiveMetadata{{RootId: 0}, {RootId: 1}}, TagReportMap: []types.TagReport{{TagName: "0"}, {TagName: "1"}}, MemberSalvationStatusMap: []types.MemberSalvationStatus{{Address: "0"}, {Address: "1"}}, JuryParticipationMap: []types.JuryParticipation{{Juror: "0"}, {Juror: "1"}}, MemberReportMap: []types.MemberReport{{Member: "0"}, {Member: "1"}}, MemberWarningList: []types.MemberWarning{{Id: 0}, {Id: 1}}, MemberWarningCount: 2,
				GovActionAppealList: []types.GovActionAppeal{{Id: 0}, {Id: 1}}, GovActionAppealCount: 2}, valid: false,
		}, {
			desc: "duplicated archivedThread",
			genState: &types.GenesisState{
				ArchivedThreadMap: []types.ArchivedThread{
					{
						RootId: 0,
					},
					{
						RootId: 0,
					},
				},
				ArchiveMetadataMap: []types.ArchiveMetadata{{RootId: 0}, {RootId: 1}}, TagReportMap: []types.TagReport{{TagName: "0"}, {TagName: "1"}}, MemberSalvationStatusMap: []types.MemberSalvationStatus{{Address: "0"}, {Address: "1"}}, JuryParticipationMap: []types.JuryParticipation{{Juror: "0"}, {Juror: "1"}}, MemberReportMap: []types.MemberReport{{Member: "0"}, {Member: "1"}}, MemberWarningList: []types.MemberWarning{{Id: 0}, {Id: 1}}, MemberWarningCount: 2,
				GovActionAppealList: []types.GovActionAppeal{{Id: 0}, {Id: 1}}, GovActionAppealCount: 2}, valid: false,
		}, {
			desc: "duplicated archiveMetadata",
			genState: &types.GenesisState{
				ArchiveMetadataMap: []types.ArchiveMetadata{
					{
						RootId: 0,
					},
					{
						RootId: 0,
					},
				},
				TagReportMap: []types.TagReport{{TagName: "0"}, {TagName: "1"}}, MemberSalvationStatusMap: []types.MemberSalvationStatus{{Address: "0"}, {Address: "1"}}, JuryParticipationMap: []types.JuryParticipation{{Juror: "0"}, {Juror: "1"}}, MemberReportMap: []types.MemberReport{{Member: "0"}, {Member: "1"}}, MemberWarningList: []types.MemberWarning{{Id: 0}, {Id: 1}}, MemberWarningCount: 2,
				GovActionAppealList: []types.GovActionAppeal{{Id: 0}, {Id: 1}}, GovActionAppealCount: 2}, valid: false,
		}, {
			desc: "duplicated tagReport",
			genState: &types.GenesisState{
				TagReportMap: []types.TagReport{
					{
						TagName: "0",
					},
					{
						TagName: "0",
					},
				},
				MemberSalvationStatusMap: []types.MemberSalvationStatus{{Address: "0"}, {Address: "1"}}, JuryParticipationMap: []types.JuryParticipation{{Juror: "0"}, {Juror: "1"}}, MemberReportMap: []types.MemberReport{{Member: "0"}, {Member: "1"}}, MemberWarningList: []types.MemberWarning{{Id: 0}, {Id: 1}}, MemberWarningCount: 2,
				GovActionAppealList: []types.GovActionAppeal{{Id: 0}, {Id: 1}}, GovActionAppealCount: 2}, valid: false,
		}, {
			desc: "duplicated memberSalvationStatus",
			genState: &types.GenesisState{
				MemberSalvationStatusMap: []types.MemberSalvationStatus{
					{
						Address: "0",
					},
					{
						Address: "0",
					},
				},
				JuryParticipationMap: []types.JuryParticipation{{Juror: "0"}, {Juror: "1"}}, MemberReportMap: []types.MemberReport{{Member: "0"}, {Member: "1"}}, MemberWarningList: []types.MemberWarning{{Id: 0}, {Id: 1}}, MemberWarningCount: 2,
				GovActionAppealList: []types.GovActionAppeal{{Id: 0}, {Id: 1}}, GovActionAppealCount: 2}, valid: false,
		}, {
			desc: "duplicated juryParticipation",
			genState: &types.GenesisState{
				JuryParticipationMap: []types.JuryParticipation{
					{
						Juror: "0",
					},
					{
						Juror: "0",
					},
				},
				MemberReportMap: []types.MemberReport{{Member: "0"}, {Member: "1"}}, MemberWarningList: []types.MemberWarning{{Id: 0}, {Id: 1}}, MemberWarningCount: 2,
				GovActionAppealList: []types.GovActionAppeal{{Id: 0}, {Id: 1}}, GovActionAppealCount: 2}, valid: false,
		}, {
			desc: "duplicated memberReport",
			genState: &types.GenesisState{
				MemberReportMap: []types.MemberReport{
					{
						Member: "0",
					},
					{
						Member: "0",
					},
				},
				MemberWarningList: []types.MemberWarning{{Id: 0}, {Id: 1}}, MemberWarningCount: 2,
				GovActionAppealList: []types.GovActionAppeal{{Id: 0}, {Id: 1}}, GovActionAppealCount: 2}, valid: false,
		}, {
			desc: "duplicated memberWarning",
			genState: &types.GenesisState{
				MemberWarningList: []types.MemberWarning{
					{
						Id: 0,
					},
					{
						Id: 0,
					},
				},
				GovActionAppealList: []types.GovActionAppeal{{Id: 0}, {Id: 1}}, GovActionAppealCount: 2,
			}, valid: false,
		}, {
			desc: "invalid memberWarning count",
			genState: &types.GenesisState{
				MemberWarningList: []types.MemberWarning{
					{
						Id: 1,
					},
				},
				MemberWarningCount: 0,
				GovActionAppealList: []types.GovActionAppeal{{Id: 0}, {Id: 1}}, GovActionAppealCount: 2,
			}, valid: false,
		}, {
			desc: "duplicated hrActionAppeal",
			genState: &types.GenesisState{
				GovActionAppealList: []types.GovActionAppeal{
					{
						Id: 0,
					},
					{
						Id: 0,
					},
				},
			},
			valid: false,
		}, {
			desc: "invalid hrActionAppeal count",
			genState: &types.GenesisState{
				GovActionAppealList: []types.GovActionAppeal{
					{
						Id: 1,
					},
				},
				GovActionAppealCount: 0,
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

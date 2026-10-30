package keeper_test

import (
	"context"
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

// Member-report and gov-action-appeal resolutions zero members and slash
// bonds, so a single Commons Operations Committee member must not be able to
// apply them: only the committee's policy address (an executed proposal), its
// council or x/gov.
func TestResolversRequireCommitteePolicy(t *testing.T) {
	gate := func(f *fixture) (policy, member string) {
		var err error
		policy, err = f.addressCodec.BytesToString(sdk.AccAddress([]byte("ops_policy______")))
		require.NoError(t, err)
		member, err = f.addressCodec.BytesToString(sdk.AccAddress([]byte("ops_member______")))
		require.NoError(t, err)
		// Every committee member is "council authorized"; only the policy
		// address is the committee policy.
		f.commonsKeeper.IsCouncilAuthorizedFn = func(context.Context, string, string, string) bool { return true }
		f.commonsKeeper.IsCouncilOrCommitteePolicyFn = func(_ context.Context, addr, council, committee string) bool {
			return addr == policy && council == "commons" && committee == "operations"
		}
		return policy, member
	}

	t.Run("member report", func(t *testing.T) {
		f := initFixture(t)
		policy, member := gate(f)
		ms := keeper.NewMsgServerImpl(f.keeper)
		subject, err := f.addressCodec.BytesToString(sdk.AccAddress([]byte("report_subject__")))
		require.NoError(t, err)

		_, err = ms.ResolveMemberReport(f.ctx, &types.MsgResolveMemberReport{Creator: member, Member: subject})
		require.ErrorIs(t, err, types.ErrNotGovAuthority, "a single committee member must be refused")

		// The policy address and x/gov pass the gate (and then find no report).
		_, err = ms.ResolveMemberReport(f.ctx, &types.MsgResolveMemberReport{Creator: policy, Member: subject})
		require.ErrorIs(t, err, types.ErrReportNotFound)
		gov, err := f.addressCodec.BytesToString(f.keeper.GetAuthority())
		require.NoError(t, err)
		_, err = ms.ResolveMemberReport(f.ctx, &types.MsgResolveMemberReport{Creator: gov, Member: subject})
		require.ErrorIs(t, err, types.ErrReportNotFound)
	})

	t.Run("gov action appeal", func(t *testing.T) {
		rf := setupResolveFixture(t)
		policy, member := gate(rf.f)
		appealID, _ := findAppeal(t, rf.f, rf.appellantStr)

		_, err := rf.ms.ResolveGovActionAppeal(rf.f.ctx, &types.MsgResolveGovActionAppeal{
			Resolver: member, AppealId: appealID,
			Verdict: types.GovAppealStatus_GOV_APPEAL_STATUS_UPHELD, Reason: "lone member",
		})
		require.ErrorIs(t, err, types.ErrNotGovAuthority, "a single committee member must be refused")

		_, err = rf.ms.ResolveGovActionAppeal(rf.f.ctx, &types.MsgResolveGovActionAppeal{
			Resolver: policy, AppealId: appealID,
			Verdict: types.GovAppealStatus_GOV_APPEAL_STATUS_UPHELD, Reason: "committee proposal",
		})
		require.NoError(t, err)
	})

	t.Run("interim approval", func(t *testing.T) {
		f := initFixture(t)
		policy, member := gate(f)
		ms := keeper.NewMsgServerImpl(f.keeper)
		assignee := sdk.AccAddress([]byte("interim_assignee"))
		assigneeStr, err := f.addressCodec.BytesToString(assignee)
		require.NoError(t, err)
		require.NoError(t, f.keeper.Member.Set(f.ctx, assigneeStr, types.Member{
			Address:          assigneeStr,
			DreamBalance:     keeper.PtrInt(math.ZeroInt()),
			StakedDream:      keeper.PtrInt(math.ZeroInt()),
			LifetimeEarned:   keeper.PtrInt(math.ZeroInt()),
			LifetimeBurned:   keeper.PtrInt(math.ZeroInt()),
			ReputationScores: map[string]string{"tag": "100.0"},
		}))
		interimID, err := f.keeper.CreateInterimWork(f.ctx, types.InterimType_INTERIM_TYPE_CONTRIBUTION_REVIEW,
			[]string{assigneeStr}, "", 1, "initiative", types.InterimComplexity_INTERIM_COMPLEXITY_STANDARD, 12345)
		require.NoError(t, err)
		require.NoError(t, f.keeper.SubmitInterimWork(f.ctx, interimID, assignee, "uri", "notes"))

		_, err = ms.ApproveInterim(f.ctx, &types.MsgApproveInterim{Creator: member, InterimId: interimID, Approved: true})
		require.ErrorIs(t, err, types.ErrNotGovAuthority, "a single committee member must not pay an interim")
		interim, err := f.keeper.GetInterim(f.ctx, interimID)
		require.NoError(t, err)
		require.NotEqual(t, types.InterimStatus_INTERIM_STATUS_COMPLETED, interim.Status)

		_, err = ms.ApproveInterim(f.ctx, &types.MsgApproveInterim{Creator: policy, InterimId: interimID, Approved: true})
		require.NoError(t, err)
		interim, err = f.keeper.GetInterim(f.ctx, interimID)
		require.NoError(t, err)
		require.Equal(t, types.InterimStatus_INTERIM_STATUS_COMPLETED, interim.Status)
	})

	t.Run("adjudication completion", func(t *testing.T) {
		f := initFixture(t)
		policy, member := gate(f)
		ms := keeper.NewMsgServerImpl(f.keeper)
		interimID, err := f.keeper.CreateInterimWork(f.ctx, types.InterimType_INTERIM_TYPE_ADJUDICATION,
			[]string{}, "", 0, "challenge", types.InterimComplexity_INTERIM_COMPLEXITY_SIMPLE, 12345)
		require.NoError(t, err)

		_, err = ms.CompleteInterim(f.ctx, &types.MsgCompleteInterim{Creator: member, InterimId: interimID})
		require.ErrorIs(t, err, types.ErrNotGovAuthority, "a single committee member must not settle an adjudication")

		_, err = ms.CompleteInterim(f.ctx, &types.MsgCompleteInterim{Creator: policy, InterimId: interimID, CompletionNotes: "settled"})
		require.NoError(t, err)
	})
}

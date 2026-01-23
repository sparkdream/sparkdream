package keeper_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

func TestMsgServerCompleteInterim(t *testing.T) {
	t.Run("invalid creator address", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		_, err := ms.CompleteInterim(f.ctx, &types.MsgCompleteInterim{
			Creator:         "invalid-address",
			InterimId:       1,
			CompletionNotes: "Done",
		})

		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid creator address")
	})

	t.Run("non-existent interim", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		creator := sdk.AccAddress([]byte("creator_________"))
		creatorStr, err := f.addressCodec.BytesToString(creator)
		require.NoError(t, err)

		_, err = ms.CompleteInterim(f.ctx, &types.MsgCompleteInterim{
			Creator:         creatorStr,
			InterimId:       99999,
			CompletionNotes: "Done",
		})

		require.Error(t, err)
		require.Contains(t, err.Error(), "interim not found")
	})

	t.Run("regular interim - unauthorized non-assignee", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)
		k := f.keeper
		ctx := f.ctx

		// Setup: create interim with one assignee
		assignee := TestAddrAssignee
		assigneeStr, err := f.addressCodec.BytesToString(assignee)
		require.NoError(t, err)

		SetupBasicMember(t, &k, sdk.UnwrapSDKContext(ctx), assignee)

		interimID, err := k.CreateInterimWork(
			ctx,
			types.InterimType_INTERIM_TYPE_CONTRIBUTION_REVIEW,
			[]string{assigneeStr},
			"",
			1,
			"initiative",
			types.InterimComplexity_INTERIM_COMPLEXITY_STANDARD,
			12345,
		)
		require.NoError(t, err)

		// Try to complete as non-assignee
		nonAssignee := TestAddrCreator
		nonAssigneeStr, err := f.addressCodec.BytesToString(nonAssignee)
		require.NoError(t, err)

		_, err = ms.CompleteInterim(ctx, &types.MsgCompleteInterim{
			Creator:         nonAssigneeStr,
			InterimId:       interimID,
			CompletionNotes: "Trying to complete without being assignee",
		})

		require.Error(t, err)
		require.Contains(t, err.Error(), "only assignees can complete this interim")
	})

	t.Run("regular interim - successful completion by assignee", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)
		k := f.keeper
		ctx := f.ctx

		// Setup: create interim and assignee
		assignee := TestAddrAssignee
		assigneeStr, err := f.addressCodec.BytesToString(assignee)
		require.NoError(t, err)

		SetupBasicMember(t, &k, sdk.UnwrapSDKContext(ctx), assignee)

		interimID, err := k.CreateInterimWork(
			ctx,
			types.InterimType_INTERIM_TYPE_CONTRIBUTION_REVIEW,
			[]string{assigneeStr},
			"",
			1,
			"initiative",
			types.InterimComplexity_INTERIM_COMPLEXITY_STANDARD,
			12345,
		)
		require.NoError(t, err)

		// Submit work first
		err = k.SubmitInterimWork(ctx, interimID, assignee, "uri", "work comments")
		require.NoError(t, err)

		// Complete interim as the assignee
		_, err = ms.CompleteInterim(ctx, &types.MsgCompleteInterim{
			Creator:         assigneeStr,
			InterimId:       interimID,
			CompletionNotes: "Work completed successfully",
		})
		require.NoError(t, err)

		// Verify interim was completed
		interim, err := k.GetInterim(ctx, interimID)
		require.NoError(t, err)
		require.Equal(t, types.InterimStatus_INTERIM_STATUS_COMPLETED, interim.Status)
		require.Equal(t, "Work completed successfully", interim.CompletionNotes)
	})

	t.Run("adjudication interim - unauthorized non-committee member", func(t *testing.T) {
		// Use a fixture that does NOT authorize anyone as operations committee
		f := initFixture(t, WithAuthorizationPolicy(NeverAuthorized))
		ms := keeper.NewMsgServerImpl(f.keeper)
		k := f.keeper
		ctx := f.ctx

		// Setup assignee member
		assignee := TestAddrAssignee
		assigneeStr, err := f.addressCodec.BytesToString(assignee)
		require.NoError(t, err)

		SetupBasicMember(t, &k, sdk.UnwrapSDKContext(ctx), assignee)

		// Create ADJUDICATION interim
		interimID, err := k.CreateInterimWork(
			ctx,
			types.InterimType_INTERIM_TYPE_ADJUDICATION,
			[]string{assigneeStr},
			"technical",
			1,
			"challenge",
			types.InterimComplexity_INTERIM_COMPLEXITY_COMPLEX,
			12345,
		)
		require.NoError(t, err)

		// Try to complete as non-committee member
		nonCommittee := TestAddrCreator
		nonCommitteeStr, err := f.addressCodec.BytesToString(nonCommittee)
		require.NoError(t, err)

		_, err = ms.CompleteInterim(ctx, &types.MsgCompleteInterim{
			Creator:         nonCommitteeStr,
			InterimId:       interimID,
			CompletionNotes: "REJECTED - insufficient evidence",
		})

		require.Error(t, err)
		require.Contains(t, err.Error(), "only technical committee members can complete ADJUDICATION interims")
	})

	t.Run("adjudication interim - successful completion by committee member", func(t *testing.T) {
		// Setup committee member authorization
		committeeMember := TestAddrApprover
		f := initFixture(t, WithAuthorizationPolicy(AuthorizeAddresses(committeeMember)))
		ms := keeper.NewMsgServerImpl(f.keeper)
		k := f.keeper
		ctx := f.ctx

		// Setup assignee member
		assignee := TestAddrAssignee
		assigneeStr, err := f.addressCodec.BytesToString(assignee)
		require.NoError(t, err)

		SetupBasicMember(t, &k, sdk.UnwrapSDKContext(ctx), assignee)

		// Create ADJUDICATION interim
		interimID, err := k.CreateInterimWork(
			ctx,
			types.InterimType_INTERIM_TYPE_ADJUDICATION,
			[]string{assigneeStr},
			"technical",
			1,
			"challenge",
			types.InterimComplexity_INTERIM_COMPLEXITY_COMPLEX,
			12345,
		)
		require.NoError(t, err)

		// Complete as committee member
		committeeStr, err := f.addressCodec.BytesToString(committeeMember)
		require.NoError(t, err)

		_, err = ms.CompleteInterim(ctx, &types.MsgCompleteInterim{
			Creator:         committeeStr,
			InterimId:       interimID,
			CompletionNotes: "UPHELD - evidence supports challenge",
		})
		require.NoError(t, err)

		// Verify interim was completed
		interim, err := k.GetInterim(ctx, interimID)
		require.NoError(t, err)
		require.Equal(t, types.InterimStatus_INTERIM_STATUS_COMPLETED, interim.Status)
		require.Equal(t, "UPHELD - evidence supports challenge", interim.CompletionNotes)
	})

	t.Run("regular interim - multiple assignees completion", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)
		k := f.keeper
		ctx := f.ctx

		// Setup: create interim with multiple assignees
		assignee1 := TestAddrMember1
		assignee2 := TestAddrMember2
		assignee1Str, err := f.addressCodec.BytesToString(assignee1)
		require.NoError(t, err)
		assignee2Str, err := f.addressCodec.BytesToString(assignee2)
		require.NoError(t, err)

		SetupBasicMember(t, &k, sdk.UnwrapSDKContext(ctx), assignee1)
		SetupBasicMember(t, &k, sdk.UnwrapSDKContext(ctx), assignee2)

		interimID, err := k.CreateInterimWork(
			ctx,
			types.InterimType_INTERIM_TYPE_CONTRIBUTION_REVIEW,
			[]string{assignee1Str, assignee2Str},
			"",
			1,
			"initiative",
			types.InterimComplexity_INTERIM_COMPLEXITY_STANDARD,
			12345,
		)
		require.NoError(t, err)

		// Submit work
		err = k.SubmitInterimWork(ctx, interimID, assignee1, "uri", "work comments")
		require.NoError(t, err)

		// First assignee can complete
		_, err = ms.CompleteInterim(ctx, &types.MsgCompleteInterim{
			Creator:         assignee1Str,
			InterimId:       interimID,
			CompletionNotes: "Completed by first assignee",
		})
		require.NoError(t, err)

		// Verify interim was completed
		interim, err := k.GetInterim(ctx, interimID)
		require.NoError(t, err)
		require.Equal(t, types.InterimStatus_INTERIM_STATUS_COMPLETED, interim.Status)
	})

	t.Run("regular interim - second assignee can complete", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)
		k := f.keeper
		ctx := f.ctx

		// Setup: create interim with multiple assignees
		assignee1 := TestAddrMember1
		assignee2 := TestAddrMember2
		assignee1Str, err := f.addressCodec.BytesToString(assignee1)
		require.NoError(t, err)
		assignee2Str, err := f.addressCodec.BytesToString(assignee2)
		require.NoError(t, err)

		SetupBasicMember(t, &k, sdk.UnwrapSDKContext(ctx), assignee1)
		SetupBasicMember(t, &k, sdk.UnwrapSDKContext(ctx), assignee2)

		interimID, err := k.CreateInterimWork(
			ctx,
			types.InterimType_INTERIM_TYPE_CONTRIBUTION_REVIEW,
			[]string{assignee1Str, assignee2Str},
			"",
			1,
			"initiative",
			types.InterimComplexity_INTERIM_COMPLEXITY_STANDARD,
			12345,
		)
		require.NoError(t, err)

		// Submit work
		err = k.SubmitInterimWork(ctx, interimID, assignee2, "uri", "work comments")
		require.NoError(t, err)

		// Second assignee can complete
		_, err = ms.CompleteInterim(ctx, &types.MsgCompleteInterim{
			Creator:         assignee2Str,
			InterimId:       interimID,
			CompletionNotes: "Completed by second assignee",
		})
		require.NoError(t, err)

		// Verify interim was completed
		interim, err := k.GetInterim(ctx, interimID)
		require.NoError(t, err)
		require.Equal(t, types.InterimStatus_INTERIM_STATUS_COMPLETED, interim.Status)
	})

	t.Run("completion rewards DREAM to assignees", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)
		k := f.keeper
		ctx := f.ctx

		// Setup: create interim and assignee with initial DREAM balance
		assignee := TestAddrAssignee
		assigneeStr, err := f.addressCodec.BytesToString(assignee)
		require.NoError(t, err)

		// Setup member with specific initial DREAM balance
		cfg := DefaultMemberConfig(assignee)
		cfg.DreamBalance = 100
		SetupMember(t, &k, sdk.UnwrapSDKContext(ctx), cfg)

		// Get initial balance
		memberBefore, err := k.Member.Get(ctx, assigneeStr)
		require.NoError(t, err)
		initialBalance := memberBefore.DreamBalance.Int64()

		interimID, err := k.CreateInterimWork(
			ctx,
			types.InterimType_INTERIM_TYPE_CONTRIBUTION_REVIEW,
			[]string{assigneeStr},
			"",
			1,
			"initiative",
			types.InterimComplexity_INTERIM_COMPLEXITY_STANDARD,
			12345,
		)
		require.NoError(t, err)

		// Get the interim to know the budget
		interim, err := k.GetInterim(ctx, interimID)
		require.NoError(t, err)
		expectedReward := interim.Budget.Int64()

		// Submit work
		err = k.SubmitInterimWork(ctx, interimID, assignee, "uri", "work comments")
		require.NoError(t, err)

		// Complete interim as the assignee
		_, err = ms.CompleteInterim(ctx, &types.MsgCompleteInterim{
			Creator:         assigneeStr,
			InterimId:       interimID,
			CompletionNotes: "Work completed successfully",
		})
		require.NoError(t, err)

		// Verify DREAM was minted to assignee
		memberAfter, err := k.Member.Get(ctx, assigneeStr)
		require.NoError(t, err)
		finalBalance := memberAfter.DreamBalance.Int64()

		require.Equal(t, initialBalance+expectedReward, finalBalance,
			"DREAM balance should increase by interim budget")
	})

	t.Run("adjudication interim does not reward DREAM", func(t *testing.T) {
		// Setup committee member authorization
		committeeMember := TestAddrApprover
		f := initFixture(t, WithAuthorizationPolicy(AuthorizeAddresses(committeeMember)))
		ms := keeper.NewMsgServerImpl(f.keeper)
		k := f.keeper
		ctx := f.ctx

		// Setup assignee member with initial DREAM
		assignee := TestAddrAssignee
		assigneeStr, err := f.addressCodec.BytesToString(assignee)
		require.NoError(t, err)

		cfg := DefaultMemberConfig(assignee)
		cfg.DreamBalance = 100
		SetupMember(t, &k, sdk.UnwrapSDKContext(ctx), cfg)

		// Get initial balance
		memberBefore, err := k.Member.Get(ctx, assigneeStr)
		require.NoError(t, err)
		initialBalance := memberBefore.DreamBalance.Int64()

		// Create ADJUDICATION interim
		interimID, err := k.CreateInterimWork(
			ctx,
			types.InterimType_INTERIM_TYPE_ADJUDICATION,
			[]string{assigneeStr},
			"technical",
			1,
			"challenge",
			types.InterimComplexity_INTERIM_COMPLEXITY_COMPLEX,
			12345,
		)
		require.NoError(t, err)

		// Complete as committee member
		committeeStr, err := f.addressCodec.BytesToString(committeeMember)
		require.NoError(t, err)

		_, err = ms.CompleteInterim(ctx, &types.MsgCompleteInterim{
			Creator:         committeeStr,
			InterimId:       interimID,
			CompletionNotes: "REJECTED - no DREAM reward for adjudication",
		})
		require.NoError(t, err)

		// Verify DREAM was NOT minted to assignee (adjudication interims skip payment)
		memberAfter, err := k.Member.Get(ctx, assigneeStr)
		require.NoError(t, err)
		finalBalance := memberAfter.DreamBalance.Int64()

		require.Equal(t, initialBalance, finalBalance,
			"DREAM balance should not change for adjudication interims")
	})
}

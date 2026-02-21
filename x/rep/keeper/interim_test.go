package keeper_test

import (
	"context"
	"fmt"
	"testing"

	"cosmossdk.io/math"
	storetypes "cosmossdk.io/store/types"
	addresscodec "github.com/cosmos/cosmos-sdk/codec/address"
	"github.com/cosmos/cosmos-sdk/runtime"
	"github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	moduletestutil "github.com/cosmos/cosmos-sdk/types/module/testutil"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/keeper"
	module "sparkdream/x/rep/module"
	"sparkdream/x/rep/types"
)

func TestCreateInterimWork(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	// Create committee member for testing
	committeeAddr := sdk.AccAddress([]byte("committee"))
	k.Member.Set(ctx, committeeAddr.String(), types.Member{
		Address:          committeeAddr.String(),
		DreamBalance:     PtrInt(math.ZeroInt()),
		StakedDream:      PtrInt(math.ZeroInt()),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		ReputationScores: map[string]string{"audit": "50.0"},
	})

	// Test creating a simple interim
	assignees := []string{committeeAddr.String()}
	interimID, err := k.CreateInterimWork(
		ctx,
		types.InterimType_INTERIM_TYPE_JURY_DUTY,
		assignees,
		"technical",
		1,
		"challenge",
		types.InterimComplexity_INTERIM_COMPLEXITY_SIMPLE,
		sdk.UnwrapSDKContext(ctx).BlockTime().Unix()+86400, // 1 day from now
	)
	require.NoError(t, err)
	require.Equal(t, uint64(1), interimID) // IDs start at 1 (0 is reserved for "unset")

	// Verify interim was created correctly
	interim, err := k.GetInterim(ctx, interimID)
	require.NoError(t, err)
	require.Equal(t, interimID, interim.Id)
	require.Equal(t, types.InterimType_INTERIM_TYPE_JURY_DUTY, interim.Type)
	require.Equal(t, assignees, interim.Assignees)
	require.Equal(t, "technical", interim.Committee)
	require.Equal(t, uint64(1), interim.ReferenceId)
	require.Equal(t, "challenge", interim.ReferenceType)
	require.Equal(t, types.InterimComplexity_INTERIM_COMPLEXITY_SIMPLE, interim.Complexity)
	require.Equal(t, types.InterimStatus_INTERIM_STATUS_PENDING, interim.Status)
	require.NotZero(t, interim.CreatedAt)

	// Verify budget based on complexity
	params, _ := k.Params.Get(ctx)
	require.Equal(t, params.SimpleComplexityBudget.String(), interim.Budget.String())
}

func TestCreateInterimWithSoloExpertBonus(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	// Create committee member
	committeeAddr := sdk.AccAddress([]byte("committee"))
	k.Member.Set(ctx, committeeAddr.String(), types.Member{
		Address:          committeeAddr.String(),
		DreamBalance:     PtrInt(math.ZeroInt()),
		StakedDream:      PtrInt(math.ZeroInt()),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		ReputationScores: map[string]string{"audit": "100.0"},
	})

	// Test solo expert interim (should get bonus)
	assignees := []string{committeeAddr.String()}
	interimID, err := k.CreateInterimWork(
		ctx,
		types.InterimType_INTERIM_TYPE_EXPERT_TESTIMONY,
		assignees,
		"commons",
		1,
		"challenge",
		types.InterimComplexity_INTERIM_COMPLEXITY_EXPERT,
		sdk.UnwrapSDKContext(ctx).BlockTime().Unix()+86400,
	)
	require.NoError(t, err)

	interim, err := k.GetInterim(ctx, interimID)
	require.NoError(t, err)

	params, _ := k.Params.Get(ctx)
	baseBudget := params.ExpertComplexityBudget
	expectedBonus := math.LegacyNewDecFromInt(baseBudget).Mul(params.SoloExpertBonusRate).TruncateInt()
	expectedBudget := baseBudget.Add(expectedBonus)

	require.Equal(t, expectedBudget.String(), interim.Budget.String())
}

func TestCreateInterimWithMultipleAssignees(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	// Create multiple committee members
	assignee1 := sdk.AccAddress([]byte("assignee1"))
	assignee2 := sdk.AccAddress([]byte("assignee2"))

	for _, addr := range []sdk.AccAddress{assignee1, assignee2} {
		k.Member.Set(ctx, addr.String(), types.Member{
			Address:          addr.String(),
			DreamBalance:     PtrInt(math.ZeroInt()),
			StakedDream:      PtrInt(math.ZeroInt()),
			LifetimeEarned:   PtrInt(math.ZeroInt()),
			LifetimeBurned:   PtrInt(math.ZeroInt()),
			ReputationScores: map[string]string{"audit": "50.0"},
		})
	}

	// Create interim with multiple assignees
	assignees := []string{assignee1.String(), assignee2.String()}
	interimID, err := k.CreateInterimWork(
		ctx,
		types.InterimType_INTERIM_TYPE_DISPUTE_MEDIATION,
		assignees,
		"commons",
		1,
		"challenge",
		types.InterimComplexity_INTERIM_COMPLEXITY_STANDARD,
		sdk.UnwrapSDKContext(ctx).BlockTime().Unix()+86400,
	)
	require.NoError(t, err)

	interim, err := k.GetInterim(ctx, interimID)
	require.NoError(t, err)
	require.Len(t, interim.Assignees, 2)
	require.Contains(t, interim.Assignees, assignee1.String())
	require.Contains(t, interim.Assignees, assignee2.String())
}

func TestGetInterim(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	// Create committee member
	committeeAddr := sdk.AccAddress([]byte("committee"))
	k.Member.Set(ctx, committeeAddr.String(), types.Member{
		Address:          committeeAddr.String(),
		DreamBalance:     PtrInt(math.ZeroInt()),
		StakedDream:      PtrInt(math.ZeroInt()),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		ReputationScores: map[string]string{"audit": "50.0"},
	})

	// Create an interim
	assignees := []string{committeeAddr.String()}
	interimID, err := k.CreateInterimWork(
		ctx,
		types.InterimType_INTERIM_TYPE_JURY_DUTY,
		assignees,
		"technical",
		1,
		"challenge",
		types.InterimComplexity_INTERIM_COMPLEXITY_SIMPLE,
		sdk.UnwrapSDKContext(ctx).BlockTime().Unix()+86400,
	)
	require.NoError(t, err)

	// Test getting interim
	interim, err := k.GetInterim(ctx, interimID)
	require.NoError(t, err)
	require.Equal(t, interimID, interim.Id)

	// Test getting non-existent interim
	_, err = k.GetInterim(ctx, 999)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not found")
}

func TestAssignInterimToMember(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	// Create committee member and assignee
	committeeAddr := sdk.AccAddress([]byte("committee"))
	assignee := sdk.AccAddress([]byte("assignee"))

	for _, addr := range []sdk.AccAddress{committeeAddr, assignee} {
		k.Member.Set(ctx, addr.String(), types.Member{
			Address:          addr.String(),
			DreamBalance:     PtrInt(math.ZeroInt()),
			StakedDream:      PtrInt(math.ZeroInt()),
			LifetimeEarned:   PtrInt(math.ZeroInt()),
			LifetimeBurned:   PtrInt(math.ZeroInt()),
			ReputationScores: map[string]string{"audit": "50.0"},
		})
	}

	// Create an interim without assignees
	committeeMember := sdk.AccAddress([]byte("comm_member"))
	k.Member.Set(ctx, committeeMember.String(), types.Member{
		Address:          committeeMember.String(),
		DreamBalance:     PtrInt(math.ZeroInt()),
		StakedDream:      PtrInt(math.ZeroInt()),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		ReputationScores: map[string]string{"audit": "50.0"},
	})

	interimID, err := k.CreateInterimWork(
		ctx,
		types.InterimType_INTERIM_TYPE_JURY_DUTY,
		[]string{committeeMember.String()},
		"technical",
		1,
		"challenge",
		types.InterimComplexity_INTERIM_COMPLEXITY_SIMPLE,
		sdk.UnwrapSDKContext(ctx).BlockTime().Unix()+86400,
	)
	require.NoError(t, err)

	// Remove initial assignee by directly updating
	interim, _ := k.GetInterim(ctx, interimID)
	interim.Assignees = []string{}
	k.UpdateInterim(ctx, interim)

	// Test assigning to a member
	err = k.AssignInterimToMember(ctx, interimID, assignee)
	require.NoError(t, err)

	// Verify assignment
	interim, err = k.GetInterim(ctx, interimID)
	require.NoError(t, err)
	require.Contains(t, interim.Assignees, assignee.String())
	require.Equal(t, types.InterimStatus_INTERIM_STATUS_IN_PROGRESS, interim.Status)
}

func TestAssignInterimToMember_NotPending(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	// Create committee member
	committeeAddr := sdk.AccAddress([]byte("committee"))
	k.Member.Set(ctx, committeeAddr.String(), types.Member{
		Address:          committeeAddr.String(),
		DreamBalance:     PtrInt(math.ZeroInt()),
		StakedDream:      PtrInt(math.ZeroInt()),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		ReputationScores: map[string]string{"audit": "50.0"},
	})

	// Create and mark interim as in progress
	assignees := []string{committeeAddr.String()}
	interimID, err := k.CreateInterimWork(
		ctx,
		types.InterimType_INTERIM_TYPE_JURY_DUTY,
		assignees,
		"technical",
		1,
		"challenge",
		types.InterimComplexity_INTERIM_COMPLEXITY_SIMPLE,
		sdk.UnwrapSDKContext(ctx).BlockTime().Unix()+86400,
	)
	require.NoError(t, err)

	// Mark as in progress
	interim, _ := k.GetInterim(ctx, interimID)
	interim.Status = types.InterimStatus_INTERIM_STATUS_IN_PROGRESS
	k.UpdateInterim(ctx, interim)

	// Try to assign - should fail
	newAssignee := sdk.AccAddress([]byte("new_assignee"))
	k.Member.Set(ctx, newAssignee.String(), types.Member{
		Address:          newAssignee.String(),
		DreamBalance:     PtrInt(math.ZeroInt()),
		StakedDream:      PtrInt(math.ZeroInt()),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		ReputationScores: map[string]string{"audit": "50.0"},
	})

	err = k.AssignInterimToMember(ctx, interimID, newAssignee)
	require.Error(t, err)
	require.Contains(t, err.Error(), "must be in PENDING status")
}

func TestAssignInterimToMember_AlreadyAssigned(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	// Create committee member
	committeeAddr := sdk.AccAddress([]byte("committee"))
	k.Member.Set(ctx, committeeAddr.String(), types.Member{
		Address:          committeeAddr.String(),
		DreamBalance:     PtrInt(math.ZeroInt()),
		StakedDream:      PtrInt(math.ZeroInt()),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		ReputationScores: map[string]string{"audit": "50.0"},
	})

	// Create interim
	assignees := []string{committeeAddr.String()}
	interimID, err := k.CreateInterimWork(
		ctx,
		types.InterimType_INTERIM_TYPE_JURY_DUTY,
		assignees,
		"technical",
		1,
		"challenge",
		types.InterimComplexity_INTERIM_COMPLEXITY_SIMPLE,
		sdk.UnwrapSDKContext(ctx).BlockTime().Unix()+86400,
	)
	require.NoError(t, err)

	// Try to assign same member again
	err = k.AssignInterimToMember(ctx, interimID, committeeAddr)
	require.NoError(t, err)

	// Verify still only one assignment
	interim, err := k.GetInterim(ctx, interimID)
	require.NoError(t, err)
	require.Len(t, interim.Assignees, 1)
	require.Contains(t, interim.Assignees, committeeAddr.String())
}

func TestAssignInterimToMember_NotAMember(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	// Create committee member
	committeeAddr := sdk.AccAddress([]byte("committee"))
	k.Member.Set(ctx, committeeAddr.String(), types.Member{
		Address:          committeeAddr.String(),
		DreamBalance:     PtrInt(math.ZeroInt()),
		StakedDream:      PtrInt(math.ZeroInt()),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		ReputationScores: map[string]string{"audit": "50.0"},
	})

	// Create interim without assignees
	committeeMember := sdk.AccAddress([]byte("comm_member"))
	k.Member.Set(ctx, committeeMember.String(), types.Member{
		Address:          committeeMember.String(),
		DreamBalance:     PtrInt(math.ZeroInt()),
		StakedDream:      PtrInt(math.ZeroInt()),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		ReputationScores: map[string]string{"audit": "50.0"},
	})

	interimID, err := k.CreateInterimWork(
		ctx,
		types.InterimType_INTERIM_TYPE_JURY_DUTY,
		[]string{committeeMember.String()},
		"technical",
		1,
		"challenge",
		types.InterimComplexity_INTERIM_COMPLEXITY_SIMPLE,
		sdk.UnwrapSDKContext(ctx).BlockTime().Unix()+86400,
	)
	require.NoError(t, err)

	// Remove initial assignee
	interim, _ := k.GetInterim(ctx, interimID)
	interim.Assignees = []string{}
	k.UpdateInterim(ctx, interim)

	// Try to assign non-member - should fail
	nonMember := sdk.AccAddress([]byte("non_member"))
	err = k.AssignInterimToMember(ctx, interimID, nonMember)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not a member")
}

func TestSubmitInterimWork(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	// Create committee member and assignee
	committeeAddr := sdk.AccAddress([]byte("committee"))
	assignee := sdk.AccAddress([]byte("assignee"))

	for _, addr := range []sdk.AccAddress{committeeAddr, assignee} {
		k.Member.Set(ctx, addr.String(), types.Member{
			Address:          addr.String(),
			DreamBalance:     PtrInt(math.ZeroInt()),
			StakedDream:      PtrInt(math.ZeroInt()),
			LifetimeEarned:   PtrInt(math.ZeroInt()),
			LifetimeBurned:   PtrInt(math.ZeroInt()),
			ReputationScores: map[string]string{"audit": "50.0"},
		})
	}

	// Create interim with assignee
	assignees := []string{assignee.String()}
	interimID, err := k.CreateInterimWork(
		ctx,
		types.InterimType_INTERIM_TYPE_JURY_DUTY,
		assignees,
		"technical",
		1,
		"challenge",
		types.InterimComplexity_INTERIM_COMPLEXITY_SIMPLE,
		sdk.UnwrapSDKContext(ctx).BlockTime().Unix()+86400,
	)
	require.NoError(t, err)

	// Submit work
	deliverableURI := "https://example.com/work/evidence.pdf"
	comments := "Completed review of evidence"
	err = k.SubmitInterimWork(ctx, interimID, assignee, deliverableURI, comments)
	require.NoError(t, err)

	// Verify submission
	interim, err := k.GetInterim(ctx, interimID)
	require.NoError(t, err)
	require.Equal(t, types.InterimStatus_INTERIM_STATUS_IN_PROGRESS, interim.Status)
	require.Equal(t, comments, interim.CompletionNotes)
}

func TestSubmitInterimWork_NotAssignee(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	// Create committee member and assignee
	committeeAddr := sdk.AccAddress([]byte("committee"))
	assignee := sdk.AccAddress([]byte("assignee"))
	nonAssignee := sdk.AccAddress([]byte("non_assignee"))

	for _, addr := range []sdk.AccAddress{committeeAddr, assignee, nonAssignee} {
		k.Member.Set(ctx, addr.String(), types.Member{
			Address:          addr.String(),
			DreamBalance:     PtrInt(math.ZeroInt()),
			StakedDream:      PtrInt(math.ZeroInt()),
			LifetimeEarned:   PtrInt(math.ZeroInt()),
			LifetimeBurned:   PtrInt(math.ZeroInt()),
			ReputationScores: map[string]string{"audit": "50.0"},
		})
	}

	// Create interim with assignee
	assignees := []string{assignee.String()}
	interimID, err := k.CreateInterimWork(
		ctx,
		types.InterimType_INTERIM_TYPE_JURY_DUTY,
		assignees,
		"technical",
		1,
		"challenge",
		types.InterimComplexity_INTERIM_COMPLEXITY_SIMPLE,
		sdk.UnwrapSDKContext(ctx).BlockTime().Unix()+86400,
	)
	require.NoError(t, err)

	// Try to submit from non-assignee
	err = k.SubmitInterimWork(ctx, interimID, nonAssignee, "uri", "comments")
	require.Error(t, err)
	require.Contains(t, err.Error(), "not an assignee")
}

func TestSubmitInterimWork_InvalidStatus(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	// Create committee member and assignee
	committeeAddr := sdk.AccAddress([]byte("committee"))
	assignee := sdk.AccAddress([]byte("assignee"))

	for _, addr := range []sdk.AccAddress{committeeAddr, assignee} {
		k.Member.Set(ctx, addr.String(), types.Member{
			Address:          addr.String(),
			DreamBalance:     PtrInt(math.ZeroInt()),
			StakedDream:      PtrInt(math.ZeroInt()),
			LifetimeEarned:   PtrInt(math.ZeroInt()),
			LifetimeBurned:   PtrInt(math.ZeroInt()),
			ReputationScores: map[string]string{"audit": "50.0"},
		})
	}

	// Create interim with assignee
	assignees := []string{assignee.String()}
	interimID, err := k.CreateInterimWork(
		ctx,
		types.InterimType_INTERIM_TYPE_JURY_DUTY,
		assignees,
		"technical",
		1,
		"challenge",
		types.InterimComplexity_INTERIM_COMPLEXITY_SIMPLE,
		sdk.UnwrapSDKContext(ctx).BlockTime().Unix()+86400,
	)
	require.NoError(t, err)

	// Mark interim as expired
	interim, _ := k.GetInterim(ctx, interimID)
	interim.Status = types.InterimStatus_INTERIM_STATUS_EXPIRED
	k.UpdateInterim(ctx, interim)

	// Try to submit work on expired interim
	err = k.SubmitInterimWork(ctx, interimID, assignee, "uri", "comments")
	require.Error(t, err)
	require.Contains(t, err.Error(), "must be in PENDING or IN_PROGRESS status")
}

func TestApproveInterim_Approved(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	// Create committee member, assignee, and approver
	committeeAddr := sdk.AccAddress([]byte("committee"))
	assignee := sdk.AccAddress([]byte("assignee"))
	approver := sdk.AccAddress([]byte("approver"))

	for _, addr := range []sdk.AccAddress{committeeAddr, assignee, approver} {
		k.Member.Set(ctx, addr.String(), types.Member{
			Address:          addr.String(),
			DreamBalance:     PtrInt(math.ZeroInt()),
			StakedDream:      PtrInt(math.ZeroInt()),
			LifetimeEarned:   PtrInt(math.ZeroInt()),
			LifetimeBurned:   PtrInt(math.ZeroInt()),
			ReputationScores: map[string]string{"audit": "50.0"},
		})
	}

	// Create interim with assignee
	assignees := []string{assignee.String()}
	interimID, err := k.CreateInterimWork(
		ctx,
		types.InterimType_INTERIM_TYPE_JURY_DUTY,
		assignees,
		"technical",
		1,
		"challenge",
		types.InterimComplexity_INTERIM_COMPLEXITY_SIMPLE,
		sdk.UnwrapSDKContext(ctx).BlockTime().Unix()+86400,
	)
	require.NoError(t, err)

	// Get expected payment amount
	interim, _ := k.GetInterim(ctx, interimID)
	expectedPayment := interim.Budget

	// Approve - default mock returns true for all committee membership
	comments := "Work verified and approved"
	err = k.ApproveInterim(ctx, interimID, approver, true, comments)
	require.NoError(t, err)

	// Verify interim was completed
	interim, err = k.GetInterim(ctx, interimID)
	require.NoError(t, err)
	require.Equal(t, types.InterimStatus_INTERIM_STATUS_COMPLETED, interim.Status)
	require.Equal(t, comments, interim.CompletionNotes)
	require.NotZero(t, interim.CompletedAt)

	// Verify payment was made to assignee
	member, err := k.Member.Get(ctx, assignee.String())
	require.NoError(t, err)
	require.Equal(t, expectedPayment.String(), member.DreamBalance.String())
	require.Equal(t, expectedPayment.String(), member.LifetimeEarned.String())
}

func TestApproveInterim_Rejected(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	// Create committee member and assignee
	committeeAddr := sdk.AccAddress([]byte("committee"))
	assignee := sdk.AccAddress([]byte("assignee"))
	approver := sdk.AccAddress([]byte("approver"))

	for _, addr := range []sdk.AccAddress{committeeAddr, assignee, approver} {
		k.Member.Set(ctx, addr.String(), types.Member{
			Address:          addr.String(),
			DreamBalance:     PtrInt(math.ZeroInt()),
			StakedDream:      PtrInt(math.ZeroInt()),
			LifetimeEarned:   PtrInt(math.ZeroInt()),
			LifetimeBurned:   PtrInt(math.ZeroInt()),
			ReputationScores: map[string]string{"audit": "50.0"},
		})
	}

	// Create interim with assignee
	assignees := []string{assignee.String()}
	interimID, err := k.CreateInterimWork(
		ctx,
		types.InterimType_INTERIM_TYPE_JURY_DUTY,
		assignees,
		"technical",
		1,
		"challenge",
		types.InterimComplexity_INTERIM_COMPLEXITY_SIMPLE,
		sdk.UnwrapSDKContext(ctx).BlockTime().Unix()+86400,
	)
	require.NoError(t, err)

	// Get initial balance
	member, _ := k.Member.Get(ctx, assignee.String())
	initialBalance := member.DreamBalance

	// Reject - default mock returns true for all committee membership
	comments := "Work does not meet standards"
	err = k.ApproveInterim(ctx, interimID, approver, false, comments)
	require.NoError(t, err)

	// Verify interim was marked as expired
	interim, err := k.GetInterim(ctx, interimID)
	require.NoError(t, err)
	require.Equal(t, types.InterimStatus_INTERIM_STATUS_EXPIRED, interim.Status)
	require.Equal(t, comments, interim.CompletionNotes)

	// Verify no payment was made
	member, err = k.Member.Get(ctx, assignee.String())
	require.NoError(t, err)
	require.Equal(t, initialBalance.String(), member.DreamBalance.String())
}

func TestApproveInterim_MultipleAssignees(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	// Create committee member and multiple assignees
	committeeAddr := sdk.AccAddress([]byte("committee"))
	assignee1 := sdk.AccAddress([]byte("assignee1"))
	assignee2 := sdk.AccAddress([]byte("assignee2"))
	approver := sdk.AccAddress([]byte("approver"))

	for _, addr := range []sdk.AccAddress{committeeAddr, assignee1, assignee2, approver} {
		k.Member.Set(ctx, addr.String(), types.Member{
			Address:          addr.String(),
			DreamBalance:     PtrInt(math.ZeroInt()),
			StakedDream:      PtrInt(math.ZeroInt()),
			LifetimeEarned:   PtrInt(math.ZeroInt()),
			LifetimeBurned:   PtrInt(math.ZeroInt()),
			ReputationScores: map[string]string{"audit": "50.0"},
		})
	}

	// Create interim with multiple assignees
	assignees := []string{assignee1.String(), assignee2.String()}
	interimID, err := k.CreateInterimWork(
		ctx,
		types.InterimType_INTERIM_TYPE_JURY_DUTY,
		assignees,
		"technical",
		1,
		"challenge",
		types.InterimComplexity_INTERIM_COMPLEXITY_STANDARD,
		sdk.UnwrapSDKContext(ctx).BlockTime().Unix()+86400,
	)
	require.NoError(t, err)

	// Get expected payment per assignee
	interim, _ := k.GetInterim(ctx, interimID)
	expectedPaymentPerAssignee := interim.Budget.QuoRaw(2)

	// Approve - default mock returns true for all committee membership
	err = k.ApproveInterim(ctx, interimID, approver, true, "approved")
	require.NoError(t, err)

	// Verify both assignees received payment
	for _, addrStr := range assignees {
		member, err := k.Member.Get(ctx, addrStr)
		require.NoError(t, err)
		require.Equal(t, expectedPaymentPerAssignee.String(), member.DreamBalance.String())
	}
}

func TestCompleteInterimDirectly(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	// Create committee member and assignee
	committeeAddr := sdk.AccAddress([]byte("committee"))
	assignee := sdk.AccAddress([]byte("assignee"))

	for _, addr := range []sdk.AccAddress{committeeAddr, assignee} {
		k.Member.Set(ctx, addr.String(), types.Member{
			Address:          addr.String(),
			DreamBalance:     PtrInt(math.ZeroInt()),
			StakedDream:      PtrInt(math.ZeroInt()),
			LifetimeEarned:   PtrInt(math.ZeroInt()),
			LifetimeBurned:   PtrInt(math.ZeroInt()),
			ReputationScores: map[string]string{"audit": "50.0"},
		})
	}

	// Create interim with assignee
	assignees := []string{assignee.String()}
	interimID, err := k.CreateInterimWork(
		ctx,
		types.InterimType_INTERIM_TYPE_JURY_DUTY,
		assignees,
		"technical",
		1,
		"challenge",
		types.InterimComplexity_INTERIM_COMPLEXITY_SIMPLE,
		sdk.UnwrapSDKContext(ctx).BlockTime().Unix()+86400,
	)
	require.NoError(t, err)

	// Get expected payment amount
	interim, _ := k.GetInterim(ctx, interimID)
	expectedPayment := interim.Budget

	// Complete directly
	notes := "Automatically completed"
	err = k.CompleteInterimDirectly(ctx, interimID, notes)
	require.NoError(t, err)

	// Verify completion
	interim, err = k.GetInterim(ctx, interimID)
	require.NoError(t, err)
	require.Equal(t, types.InterimStatus_INTERIM_STATUS_COMPLETED, interim.Status)
	require.Equal(t, notes, interim.CompletionNotes)
	require.NotZero(t, interim.CompletedAt)

	// Verify payment
	member, err := k.Member.Get(ctx, assignee.String())
	require.NoError(t, err)
	require.Equal(t, expectedPayment.String(), member.DreamBalance.String())
}

func TestExpireInterim(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	// Create committee member
	committeeAddr := sdk.AccAddress([]byte("committee"))
	k.Member.Set(ctx, committeeAddr.String(), types.Member{
		Address:          committeeAddr.String(),
		DreamBalance:     PtrInt(math.ZeroInt()),
		StakedDream:      PtrInt(math.ZeroInt()),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		ReputationScores: map[string]string{"audit": "50.0"},
	})

	// Create interim
	assignees := []string{committeeAddr.String()}
	interimID, err := k.CreateInterimWork(
		ctx,
		types.InterimType_INTERIM_TYPE_JURY_DUTY,
		assignees,
		"technical",
		1,
		"challenge",
		types.InterimComplexity_INTERIM_COMPLEXITY_SIMPLE,
		sdk.UnwrapSDKContext(ctx).BlockTime().Unix()+86400,
	)
	require.NoError(t, err)

	// Get initial balance
	member, _ := k.Member.Get(ctx, committeeAddr.String())
	initialBalance := member.DreamBalance

	// Expire interim
	err = k.ExpireInterim(ctx, interimID)
	require.NoError(t, err)

	// Verify expiration
	interim, err := k.GetInterim(ctx, interimID)
	require.NoError(t, err)
	require.Equal(t, types.InterimStatus_INTERIM_STATUS_EXPIRED, interim.Status)

	// Verify no payment was made
	member, err = k.Member.Get(ctx, committeeAddr.String())
	require.NoError(t, err)
	require.Equal(t, initialBalance.String(), member.DreamBalance.String())
}

func TestGetInterimBudget(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	// Get default params to verify expected budgets (in micro units)
	params, _ := k.Params.Get(ctx)

	tests := []struct {
		name           string
		complexity     types.InterimComplexity
		expectedBudget math.Int
	}{
		{
			name:           "Simple",
			complexity:     types.InterimComplexity_INTERIM_COMPLEXITY_SIMPLE,
			expectedBudget: params.SimpleComplexityBudget, // 50 DREAM = 50000000 uDREAM
		},
		{
			name:           "Standard",
			complexity:     types.InterimComplexity_INTERIM_COMPLEXITY_STANDARD,
			expectedBudget: params.StandardComplexityBudget, // 150 DREAM = 150000000 uDREAM
		},
		{
			name:           "Complex",
			complexity:     types.InterimComplexity_INTERIM_COMPLEXITY_COMPLEX,
			expectedBudget: params.ComplexComplexityBudget, // 400 DREAM = 400000000 uDREAM
		},
		{
			name:           "Expert",
			complexity:     types.InterimComplexity_INTERIM_COMPLEXITY_EXPERT,
			expectedBudget: params.ExpertComplexityBudget, // 1000 DREAM = 1000000000 uDREAM
		},
		{
			name:           "Epic",
			complexity:     types.InterimComplexity_INTERIM_COMPLEXITY_EPIC,
			expectedBudget: math.NewInt(2500), // Epic uses hardcoded fallback (no param yet)
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			budget := k.GetInterimBudget(ctx, tt.complexity)
			require.Equal(t, tt.expectedBudget.String(), budget.String())
		})
	}
}

func TestGetInterimBudget_WithCustomParams(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	// Set custom budgets in params
	customParams := types.DefaultParams()
	customParams.SimpleComplexityBudget = math.NewInt(100)
	customParams.StandardComplexityBudget = math.NewInt(300)
	customParams.ComplexComplexityBudget = math.NewInt(800)
	customParams.ExpertComplexityBudget = math.NewInt(2000)
	err := k.Params.Set(ctx, customParams)
	require.NoError(t, err)

	tests := []struct {
		name           string
		complexity     types.InterimComplexity
		expectedBudget math.Int
	}{
		{
			name:           "Simple custom",
			complexity:     types.InterimComplexity_INTERIM_COMPLEXITY_SIMPLE,
			expectedBudget: math.NewInt(100),
		},
		{
			name:           "Standard custom",
			complexity:     types.InterimComplexity_INTERIM_COMPLEXITY_STANDARD,
			expectedBudget: math.NewInt(300),
		},
		{
			name:           "Complex custom",
			complexity:     types.InterimComplexity_INTERIM_COMPLEXITY_COMPLEX,
			expectedBudget: math.NewInt(800),
		},
		{
			name:           "Expert custom",
			complexity:     types.InterimComplexity_INTERIM_COMPLEXITY_EXPERT,
			expectedBudget: math.NewInt(2000),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			budget := k.GetInterimBudget(ctx, tt.complexity)
			require.Equal(t, tt.expectedBudget.String(), budget.String())
		})
	}
}

func TestCreateInterimAllTypes(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	// Create committee member
	committeeAddr := sdk.AccAddress([]byte("committee"))
	k.Member.Set(ctx, committeeAddr.String(), types.Member{
		Address:          committeeAddr.String(),
		DreamBalance:     PtrInt(math.ZeroInt()),
		StakedDream:      PtrInt(math.ZeroInt()),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		ReputationScores: map[string]string{"audit": "50.0"},
	})

	assignees := []string{committeeAddr.String()}

	// Test all interim types
	interimTypes := []struct {
		name  string
		itype types.InterimType
	}{
		{"Jury Duty", types.InterimType_INTERIM_TYPE_JURY_DUTY},
		{"Expert Testimony", types.InterimType_INTERIM_TYPE_EXPERT_TESTIMONY},
		{"Dispute Mediation", types.InterimType_INTERIM_TYPE_DISPUTE_MEDIATION},
		{"Project Approval", types.InterimType_INTERIM_TYPE_PROJECT_APPROVAL},
		{"Budget Review", types.InterimType_INTERIM_TYPE_BUDGET_REVIEW},
		{"Contribution Review", types.InterimType_INTERIM_TYPE_CONTRIBUTION_REVIEW},
		{"Exception Request", types.InterimType_INTERIM_TYPE_EXCEPTION_REQUEST},
		{"Tranche Verification", types.InterimType_INTERIM_TYPE_TRANCHE_VERIFICATION},
		{"Audit", types.InterimType_INTERIM_TYPE_AUDIT},
		{"Moderation", types.InterimType_INTERIM_TYPE_MODERATION},
		{"Mentorship", types.InterimType_INTERIM_TYPE_MENTORSHIP},
		{"Adjudication", types.InterimType_INTERIM_TYPE_ADJUDICATION},
		{"Other", types.InterimType_INTERIM_TYPE_OTHER},
	}

	for _, tt := range interimTypes {
		t.Run(tt.name, func(t *testing.T) {
			interimID, err := k.CreateInterimWork(
				ctx,
				tt.itype,
				assignees,
				"technical",
				1,
				"test",
				types.InterimComplexity_INTERIM_COMPLEXITY_STANDARD,
				sdk.UnwrapSDKContext(ctx).BlockTime().Unix()+86400,
			)
			require.NoError(t, err)

			interim, err := k.GetInterim(ctx, interimID)
			require.NoError(t, err)
			require.Equal(t, tt.itype, interim.Type)
		})
	}
}

func TestApproveInterim_EmptyAssignees(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	// Create committee member and approver
	committeeAddr := sdk.AccAddress([]byte("committee"))
	approver := sdk.AccAddress([]byte("approver"))

	for _, addr := range []sdk.AccAddress{committeeAddr, approver} {
		k.Member.Set(ctx, addr.String(), types.Member{
			Address:          addr.String(),
			DreamBalance:     PtrInt(math.ZeroInt()),
			StakedDream:      PtrInt(math.ZeroInt()),
			LifetimeEarned:   PtrInt(math.ZeroInt()),
			LifetimeBurned:   PtrInt(math.ZeroInt()),
			ReputationScores: map[string]string{"audit": "50.0"},
		})
	}

	// Create interim with assignee
	assignee := sdk.AccAddress([]byte("assignee"))
	k.Member.Set(ctx, assignee.String(), types.Member{
		Address:          assignee.String(),
		DreamBalance:     PtrInt(math.ZeroInt()),
		StakedDream:      PtrInt(math.ZeroInt()),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		ReputationScores: map[string]string{"audit": "50.0"},
	})

	assignees := []string{assignee.String()}
	interimID, err := k.CreateInterimWork(
		ctx,
		types.InterimType_INTERIM_TYPE_JURY_DUTY,
		assignees,
		"technical",
		1,
		"challenge",
		types.InterimComplexity_INTERIM_COMPLEXITY_SIMPLE,
		sdk.UnwrapSDKContext(ctx).BlockTime().Unix()+86400,
	)
	require.NoError(t, err)

	// Remove assignee to test empty assignees scenario
	interim, _ := k.GetInterim(ctx, interimID)
	interim.Assignees = []string{}
	err = k.UpdateInterim(ctx, interim)
	require.NoError(t, err)

	// Approve - should handle empty assignees gracefully
	comments := "Approved with no assignees (edge case)"
	err = k.ApproveInterim(ctx, interimID, approver, true, comments)
	require.NoError(t, err)

	// Verify interim was completed
	interim, err = k.GetInterim(ctx, interimID)
	require.NoError(t, err)
	require.Equal(t, types.InterimStatus_INTERIM_STATUS_COMPLETED, interim.Status)
	require.Empty(t, interim.Assignees) // No payments made, assignees remain empty
}

func TestCompleteInterimDirectly_EmptyAssignees(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	// Create committee member and assignee
	committeeAddr := sdk.AccAddress([]byte("committee"))
	assignee := sdk.AccAddress([]byte("assignee"))

	for _, addr := range []sdk.AccAddress{committeeAddr, assignee} {
		k.Member.Set(ctx, addr.String(), types.Member{
			Address:          addr.String(),
			DreamBalance:     PtrInt(math.ZeroInt()),
			StakedDream:      PtrInt(math.ZeroInt()),
			LifetimeEarned:   PtrInt(math.ZeroInt()),
			LifetimeBurned:   PtrInt(math.ZeroInt()),
			ReputationScores: map[string]string{"audit": "50.0"},
		})
	}

	// Create interim with assignee
	assignees := []string{assignee.String()}
	interimID, err := k.CreateInterimWork(
		ctx,
		types.InterimType_INTERIM_TYPE_JURY_DUTY,
		assignees,
		"technical",
		1,
		"challenge",
		types.InterimComplexity_INTERIM_COMPLEXITY_SIMPLE,
		sdk.UnwrapSDKContext(ctx).BlockTime().Unix()+86400,
	)
	require.NoError(t, err)

	// Remove assignee to test empty assignees scenario
	interim, _ := k.GetInterim(ctx, interimID)
	interim.Assignees = []string{}
	err = k.UpdateInterim(ctx, interim)
	require.NoError(t, err)

	// Complete directly - should handle empty assignees gracefully
	notes := "Completed with no assignees (edge case)"
	err = k.CompleteInterimDirectly(ctx, interimID, notes)
	require.NoError(t, err)

	// Verify completion without payment
	interim, err = k.GetInterim(ctx, interimID)
	require.NoError(t, err)
	require.Equal(t, types.InterimStatus_INTERIM_STATUS_COMPLETED, interim.Status)
	require.Equal(t, notes, interim.CompletionNotes)
	require.NotZero(t, interim.CompletedAt)

	// Verify no payment was made to assignee
	member, err := k.Member.Get(ctx, assignee.String())
	require.NoError(t, err)
	require.Equal(t, math.ZeroInt().String(), member.DreamBalance.String())
}

func TestCreateInterimWork_EmitsEvent(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	// Create committee member for testing
	committeeAddr := sdk.AccAddress([]byte("committee"))
	k.Member.Set(ctx, committeeAddr.String(), types.Member{
		Address:          committeeAddr.String(),
		DreamBalance:     PtrInt(math.ZeroInt()),
		StakedDream:      PtrInt(math.ZeroInt()),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		ReputationScores: map[string]string{"audit": "50.0"},
	})

	assignees := []string{committeeAddr.String()}
	interimID, err := k.CreateInterimWork(
		ctx,
		types.InterimType_INTERIM_TYPE_JURY_DUTY,
		assignees,
		"technical",
		1,
		"challenge",
		types.InterimComplexity_INTERIM_COMPLEXITY_SIMPLE,
		sdk.UnwrapSDKContext(ctx).BlockTime().Unix()+86400,
	)
	require.NoError(t, err)

	// Verify event was emitted
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	events := sdkCtx.EventManager().Events()
	require.Len(t, events, 1)

	event := events[0]
	require.Equal(t, "interim_created", event.Type)
	require.Equal(t, fmt.Sprintf("%d", interimID), event.Attributes[0].Value)
	require.Equal(t, types.InterimType_INTERIM_TYPE_JURY_DUTY.String(), event.Attributes[1].Value)
	require.Equal(t, types.InterimComplexity_INTERIM_COMPLEXITY_SIMPLE.String(), event.Attributes[2].Value)
}

// TestUpdateInterim tests direct interim updates
func TestUpdateInterim(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	// Create committee member
	committeeAddr := sdk.AccAddress([]byte("committee"))
	k.Member.Set(ctx, committeeAddr.String(), types.Member{
		Address:          committeeAddr.String(),
		DreamBalance:     PtrInt(math.ZeroInt()),
		StakedDream:      PtrInt(math.ZeroInt()),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		ReputationScores: map[string]string{"audit": "50.0"},
	})

	// Create interim
	assignees := []string{committeeAddr.String()}
	interimID, err := k.CreateInterimWork(
		ctx,
		types.InterimType_INTERIM_TYPE_JURY_DUTY,
		assignees,
		"technical",
		1,
		"challenge",
		types.InterimComplexity_INTERIM_COMPLEXITY_SIMPLE,
		sdk.UnwrapSDKContext(ctx).BlockTime().Unix()+86400,
	)
	require.NoError(t, err)

	// Get and modify interim
	interim, err := k.GetInterim(ctx, interimID)
	require.NoError(t, err)

	// Change some fields
	interim.CompletionNotes = "Updated notes"
	interim.Status = types.InterimStatus_INTERIM_STATUS_IN_PROGRESS

	// Update interim
	err = k.UpdateInterim(ctx, interim)
	require.NoError(t, err)

	// Verify changes persisted
	updatedInterim, err := k.GetInterim(ctx, interimID)
	require.NoError(t, err)
	require.Equal(t, "Updated notes", updatedInterim.CompletionNotes)
	require.Equal(t, types.InterimStatus_INTERIM_STATUS_IN_PROGRESS, updatedInterim.Status)
}

// TestAssignInterimToMember_EmitsEvent verifies event emission on assignment
func TestAssignInterimToMember_EmitsEvent(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	// Create members
	committeeAddr := sdk.AccAddress([]byte("committee"))
	assignee := sdk.AccAddress([]byte("assignee"))

	for _, addr := range []sdk.AccAddress{committeeAddr, assignee} {
		k.Member.Set(ctx, addr.String(), types.Member{
			Address:          addr.String(),
			DreamBalance:     PtrInt(math.ZeroInt()),
			StakedDream:      PtrInt(math.ZeroInt()),
			LifetimeEarned:   PtrInt(math.ZeroInt()),
			LifetimeBurned:   PtrInt(math.ZeroInt()),
			ReputationScores: map[string]string{"audit": "50.0"},
		})
	}

	// Create interim with initial assignee then clear it
	interimID, err := k.CreateInterimWork(
		ctx,
		types.InterimType_INTERIM_TYPE_JURY_DUTY,
		[]string{committeeAddr.String()},
		"technical",
		1,
		"challenge",
		types.InterimComplexity_INTERIM_COMPLEXITY_SIMPLE,
		sdk.UnwrapSDKContext(ctx).BlockTime().Unix()+86400,
	)
	require.NoError(t, err)

	// Clear assignees and reset status
	interim, _ := k.GetInterim(ctx, interimID)
	interim.Assignees = []string{}
	interim.Status = types.InterimStatus_INTERIM_STATUS_PENDING
	k.UpdateInterim(ctx, interim)

	// Clear previous events
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx.EventManager().EmitEvent(sdk.NewEvent("clear"))
	sdkCtx = sdkCtx.WithEventManager(sdk.NewEventManager())
	ctx = sdkCtx

	// Assign to member
	err = k.AssignInterimToMember(ctx, interimID, assignee)
	require.NoError(t, err)

	// Verify event
	events := sdk.UnwrapSDKContext(ctx).EventManager().Events()
	require.Len(t, events, 1)
	require.Equal(t, "interim_assigned", events[0].Type)
	require.Equal(t, fmt.Sprintf("%d", interimID), events[0].Attributes[0].Value)
	require.Equal(t, assignee.String(), events[0].Attributes[1].Value)
}

// TestSubmitInterimWork_EmitsEvent verifies event emission on work submission
func TestSubmitInterimWork_EmitsEvent(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	// Create members
	committeeAddr := sdk.AccAddress([]byte("committee"))
	assignee := sdk.AccAddress([]byte("assignee"))

	for _, addr := range []sdk.AccAddress{committeeAddr, assignee} {
		k.Member.Set(ctx, addr.String(), types.Member{
			Address:          addr.String(),
			DreamBalance:     PtrInt(math.ZeroInt()),
			StakedDream:      PtrInt(math.ZeroInt()),
			LifetimeEarned:   PtrInt(math.ZeroInt()),
			LifetimeBurned:   PtrInt(math.ZeroInt()),
			ReputationScores: map[string]string{"audit": "50.0"},
		})
	}

	// Create interim
	interimID, err := k.CreateInterimWork(
		ctx,
		types.InterimType_INTERIM_TYPE_JURY_DUTY,
		[]string{assignee.String()},
		"technical",
		1,
		"challenge",
		types.InterimComplexity_INTERIM_COMPLEXITY_SIMPLE,
		sdk.UnwrapSDKContext(ctx).BlockTime().Unix()+86400,
	)
	require.NoError(t, err)

	// Clear previous events
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx = sdkCtx.WithEventManager(sdk.NewEventManager())
	ctx = sdkCtx

	// Submit work
	deliverableURI := "https://example.com/work.pdf"
	err = k.SubmitInterimWork(ctx, interimID, assignee, deliverableURI, "Done")
	require.NoError(t, err)

	// Verify event
	events := sdk.UnwrapSDKContext(ctx).EventManager().Events()
	require.Len(t, events, 1)
	require.Equal(t, "interim_work_submitted", events[0].Type)
	require.Equal(t, fmt.Sprintf("%d", interimID), events[0].Attributes[0].Value)
	require.Equal(t, assignee.String(), events[0].Attributes[1].Value)
	require.Equal(t, deliverableURI, events[0].Attributes[2].Value)
}

// TestApproveInterim_EmitsEvent verifies event emission on approval
func TestApproveInterim_EmitsEvent(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	// Create members
	committeeAddr := sdk.AccAddress([]byte("committee"))
	assignee := sdk.AccAddress([]byte("assignee"))
	approver := sdk.AccAddress([]byte("approver"))

	for _, addr := range []sdk.AccAddress{committeeAddr, assignee, approver} {
		k.Member.Set(ctx, addr.String(), types.Member{
			Address:          addr.String(),
			DreamBalance:     PtrInt(math.ZeroInt()),
			StakedDream:      PtrInt(math.ZeroInt()),
			LifetimeEarned:   PtrInt(math.ZeroInt()),
			LifetimeBurned:   PtrInt(math.ZeroInt()),
			ReputationScores: map[string]string{"audit": "50.0"},
		})
	}

	// Create interim
	interimID, err := k.CreateInterimWork(
		ctx,
		types.InterimType_INTERIM_TYPE_JURY_DUTY,
		[]string{assignee.String()},
		"technical",
		1,
		"challenge",
		types.InterimComplexity_INTERIM_COMPLEXITY_SIMPLE,
		sdk.UnwrapSDKContext(ctx).BlockTime().Unix()+86400,
	)
	require.NoError(t, err)

	// Clear previous events
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	sdkCtx = sdkCtx.WithEventManager(sdk.NewEventManager())
	ctx = sdkCtx

	// Approve
	err = k.ApproveInterim(ctx, interimID, approver, true, "Approved")
	require.NoError(t, err)

	// Verify events (should have both mint_dream and interim_approved)
	events := sdk.UnwrapSDKContext(ctx).EventManager().Events()
	require.GreaterOrEqual(t, len(events), 1)

	// Find the interim_approved event
	var approvedEvent sdk.Event
	for _, event := range events {
		if event.Type == "interim_approved" {
			approvedEvent = event
			break
		}
	}
	require.NotEmpty(t, approvedEvent.Type, "interim_approved event should be emitted")
	require.Equal(t, "interim_approved", approvedEvent.Type)
	require.Equal(t, fmt.Sprintf("%d", interimID), approvedEvent.Attributes[0].Value)
	require.Equal(t, approver.String(), approvedEvent.Attributes[1].Value)
	require.Equal(t, "true", approvedEvent.Attributes[2].Value)
}

// TestApproveInterim_UnauthorizedApprover tests rejection when approver lacks authority
func TestApproveInterim_UnauthorizedApprover(t *testing.T) {
	// Create custom fixture with mock that denies committee membership
	encCfg := moduletestutil.MakeTestEncodingConfig(module.AppModule{})
	addressCodec := addresscodec.NewBech32Codec(sdk.GetConfig().GetBech32AccountAddrPrefix())
	storeKey := storetypes.NewKVStoreKey(types.StoreKey)
	storeService := runtime.NewKVStoreService(storeKey)
	testCtx := testutil.DefaultContextWithDB(t, storeKey, storetypes.NewTransientStoreKey("transient_test")).Ctx
	authority := authtypes.NewModuleAddress(types.GovModuleName)

	// Mock that denies Operations Committee membership
	commonsKeeper := mockCommonsKeeper{
		IsCommitteeMemberFn: func(ctx context.Context, address sdk.AccAddress, council string, committee string) (bool, error) {
			// Deny Operations Committee membership
			if committee == "operations" {
				return false, nil
			}
			return true, nil
		},
	}

	// Mock SeasonKeeper
	seasonKeeper := &mockSeasonKeeper{}

	testKeeper := keeper.NewKeeper(
		storeService,
		encCfg.Codec,
		addressCodec,
		authority,
		nil,
		nil,
		commonsKeeper,
		seasonKeeper,
		nil, // voteKeeper
	)

	// Initialize params
	err := testKeeper.Params.Set(testCtx, types.DefaultParams())
	require.NoError(t, err)

	// Create members
	committeeAddr := sdk.AccAddress([]byte("committee"))
	assignee := sdk.AccAddress([]byte("assignee"))
	unauthorizedApprover := sdk.AccAddress([]byte("unauthorized"))

	for _, addr := range []sdk.AccAddress{committeeAddr, assignee, unauthorizedApprover} {
		testKeeper.Member.Set(testCtx, addr.String(), types.Member{
			Address:          addr.String(),
			DreamBalance:     PtrInt(math.ZeroInt()),
			StakedDream:      PtrInt(math.ZeroInt()),
			LifetimeEarned:   PtrInt(math.ZeroInt()),
			LifetimeBurned:   PtrInt(math.ZeroInt()),
			ReputationScores: map[string]string{"audit": "50.0"},
		})
	}

	// Create interim
	interimID, err := testKeeper.CreateInterimWork(
		testCtx,
		types.InterimType_INTERIM_TYPE_JURY_DUTY,
		[]string{assignee.String()},
		"technical",
		1,
		"challenge",
		types.InterimComplexity_INTERIM_COMPLEXITY_SIMPLE,
		sdk.UnwrapSDKContext(testCtx).BlockTime().Unix()+86400,
	)
	require.NoError(t, err)

	// Try to approve with unauthorized approver
	err = testKeeper.ApproveInterim(testCtx, interimID, unauthorizedApprover, true, "Approved")
	require.Error(t, err)
	require.Contains(t, err.Error(), "not authorized")
	require.Contains(t, err.Error(), "Operations Committee")
}

// TestCreateInterimWork_MultipleExpertAssignees_NoBonus verifies no bonus for multiple experts
func TestCreateInterimWork_MultipleExpertAssignees_NoBonus(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	// Create multiple expert members
	expert1 := sdk.AccAddress([]byte("expert1"))
	expert2 := sdk.AccAddress([]byte("expert2"))

	for _, addr := range []sdk.AccAddress{expert1, expert2} {
		k.Member.Set(ctx, addr.String(), types.Member{
			Address:          addr.String(),
			DreamBalance:     PtrInt(math.ZeroInt()),
			StakedDream:      PtrInt(math.ZeroInt()),
			LifetimeEarned:   PtrInt(math.ZeroInt()),
			LifetimeBurned:   PtrInt(math.ZeroInt()),
			ReputationScores: map[string]string{"audit": "100.0"},
		})
	}

	// Create expert complexity interim with multiple assignees
	assignees := []string{expert1.String(), expert2.String()}
	interimID, err := k.CreateInterimWork(
		ctx,
		types.InterimType_INTERIM_TYPE_EXPERT_TESTIMONY,
		assignees,
		"commons",
		1,
		"challenge",
		types.InterimComplexity_INTERIM_COMPLEXITY_EXPERT,
		sdk.UnwrapSDKContext(ctx).BlockTime().Unix()+86400,
	)
	require.NoError(t, err)

	// Verify budget is base without bonus
	interim, err := k.GetInterim(ctx, interimID)
	require.NoError(t, err)

	params, _ := k.Params.Get(ctx)
	baseBudget := params.ExpertComplexityBudget

	// Should get base budget WITHOUT solo expert bonus
	require.Equal(t, baseBudget.String(), interim.Budget.String())
}

// TestApproveInterim_OddBudgetDivision tests budget division with remainder
func TestApproveInterim_OddBudgetDivision(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	// Create 3 assignees
	assignee1 := sdk.AccAddress([]byte("assignee1"))
	assignee2 := sdk.AccAddress([]byte("assignee2"))
	assignee3 := sdk.AccAddress([]byte("assignee3"))
	approver := sdk.AccAddress([]byte("approver"))

	for _, addr := range []sdk.AccAddress{assignee1, assignee2, assignee3, approver} {
		k.Member.Set(ctx, addr.String(), types.Member{
			Address:          addr.String(),
			DreamBalance:     PtrInt(math.ZeroInt()),
			StakedDream:      PtrInt(math.ZeroInt()),
			LifetimeEarned:   PtrInt(math.ZeroInt()),
			LifetimeBurned:   PtrInt(math.ZeroInt()),
			ReputationScores: map[string]string{"audit": "50.0"},
		})
	}

	// Create interim with 3 assignees and STANDARD budget (150)
	// 150 / 3 = 50 each (no remainder in this case)
	// Let's use SIMPLE budget (50) for 3 people: 50 / 3 = 16 each, 2 remainder lost
	assignees := []string{assignee1.String(), assignee2.String(), assignee3.String()}
	interimID, err := k.CreateInterimWork(
		ctx,
		types.InterimType_INTERIM_TYPE_JURY_DUTY,
		assignees,
		"technical",
		1,
		"challenge",
		types.InterimComplexity_INTERIM_COMPLEXITY_SIMPLE, // 50 DREAM
		sdk.UnwrapSDKContext(ctx).BlockTime().Unix()+86400,
	)
	require.NoError(t, err)

	// Get budget
	interim, _ := k.GetInterim(ctx, interimID)
	totalBudget := interim.Budget
	expectedPerAssignee := totalBudget.QuoRaw(3) // 50 / 3 = 16 (integer division)

	// Approve
	err = k.ApproveInterim(ctx, interimID, approver, true, "approved")
	require.NoError(t, err)

	// Verify each assignee got the truncated amount
	for _, addrStr := range assignees {
		member, err := k.Member.Get(ctx, addrStr)
		require.NoError(t, err)
		require.Equal(t, expectedPerAssignee.String(), member.DreamBalance.String())
	}

	// Document: 50 DREAM total, 3 assignees = 16 each = 48 total paid, 2 DREAM lost to rounding
	totalPaid := expectedPerAssignee.MulRaw(3)
	remainder := totalBudget.Sub(totalPaid)
	t.Logf("Total budget: %s, Per assignee: %s, Total paid: %s, Lost to rounding: %s",
		totalBudget.String(), expectedPerAssignee.String(), totalPaid.String(), remainder.String())
}

// TestGetInterimBudget_UnknownComplexity tests fallback for unknown complexity
func TestGetInterimBudget_UnknownComplexity(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	// Test with invalid complexity value (999)
	unknownComplexity := types.InterimComplexity(999)
	budget := k.GetInterimBudget(ctx, unknownComplexity)

	// Should return default fallback (150)
	require.Equal(t, math.NewInt(150).String(), budget.String())
}

// TestCreateInterimWork_SequentialIDs verifies ID sequencing
func TestCreateInterimWork_SequentialIDs(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	// Create committee member
	committeeAddr := sdk.AccAddress([]byte("committee"))
	k.Member.Set(ctx, committeeAddr.String(), types.Member{
		Address:          committeeAddr.String(),
		DreamBalance:     PtrInt(math.ZeroInt()),
		StakedDream:      PtrInt(math.ZeroInt()),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		ReputationScores: map[string]string{"audit": "50.0"},
	})

	assignees := []string{committeeAddr.String()}
	deadline := sdk.UnwrapSDKContext(ctx).BlockTime().Unix() + 86400

	// Create 5 interims and verify sequential IDs (IDs start at 1)
	for i := 0; i < 5; i++ {
		interimID, err := k.CreateInterimWork(
			ctx,
			types.InterimType_INTERIM_TYPE_JURY_DUTY,
			assignees,
			"technical",
			1,
			"challenge",
			types.InterimComplexity_INTERIM_COMPLEXITY_SIMPLE,
			deadline,
		)
		require.NoError(t, err)
		require.Equal(t, uint64(i+1), interimID, "Expected sequential ID %d", i+1)
	}
}

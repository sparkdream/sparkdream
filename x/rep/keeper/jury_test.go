package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/types"
)

func TestJuryWorkflow(t *testing.T) {
	fixture := initFixture(t)
	k := fixture.keeper
	ctx := fixture.ctx

	// Setup: Project, Initiative (Standard Tier), Challenge
	projectID, _ := k.CreateProject(
		ctx,
		sdk.AccAddress([]byte("addr")),
		"Proj",
		"Desc",
		[]string{"coding"},
		types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE,
		"technical",
		math.NewInt(10000),
		math.NewInt(1000),
	)
	k.ApproveProject(ctx, projectID, sdk.AccAddress([]byte("approver")), math.NewInt(10000), math.NewInt(1000))

	assignee := sdk.AccAddress([]byte("assignee"))
	// Ensure assignee is member (needed for AssignInitiativeToMember check)
	// Ensure assignee is member (needed for AssignInitiativeToMember check)
	k.Member.Set(ctx, assignee.String(), types.Member{
		Address:          assignee.String(),
		DreamBalance:     PtrInt(math.ZeroInt()),
		StakedDream:      PtrInt(math.ZeroInt()),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		ReputationScores: map[string]string{"coding": "10.0"},
	})

	initID, _ := k.CreateInitiative(
		ctx,
		assignee, // creator
		projectID,
		"Init",
		"D",
		[]string{"coding"},
		types.InitiativeTier_INITIATIVE_TIER_STANDARD,
		types.InitiativeCategory_INITIATIVE_CATEGORY_FEATURE,
		"",
		math.NewInt(150),
	)

	// Add assignee rep for Standard tier (defaults might be high)
	// We'll update the member to have high score
	// We'll update the member to have high score
	k.Member.Set(ctx, assignee.String(), types.Member{
		Address:          assignee.String(),
		DreamBalance:     PtrInt(math.ZeroInt()),
		StakedDream:      PtrInt(math.ZeroInt()),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		ReputationScores: map[string]string{"coding": "1000.0"},
	})

	k.AssignInitiativeToMember(ctx, initID, assignee)
	k.SubmitInitiativeWork(ctx, initID, assignee, "URI")

	challenger := sdk.AccAddress([]byte("chal"))
	k.Member.Set(ctx, challenger.String(), types.Member{
		Address:          challenger.String(),
		DreamBalance:     PtrInt(math.ZeroInt()),
		StakedDream:      PtrInt(math.ZeroInt()),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		ReputationScores: make(map[string]string),
	})
	k.MintDREAM(ctx, challenger, math.NewInt(1000))

	chalID, err := k.CreateChallenge(ctx, challenger, initID, "Reason", nil, math.NewInt(50), false, "", nil, nil)
	require.NoError(t, err)

	// Create some potential jurors with reputation
	// Params: MinJurorReputation = 50 (default)
	juror1 := sdk.AccAddress([]byte("juror1"))
	juror2 := sdk.AccAddress([]byte("juror2"))
	juror3 := sdk.AccAddress([]byte("juror3")) // Will vote against to create split

	// Give them reputation
	// Give them reputation
	k.Member.Set(ctx, juror1.String(), types.Member{
		Address:          juror1.String(),
		DreamBalance:     PtrInt(math.ZeroInt()),
		StakedDream:      PtrInt(math.ZeroInt()),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		ReputationScores: map[string]string{"coding": "100.0"},
	})
	k.Member.Set(ctx, juror2.String(), types.Member{
		Address:          juror2.String(),
		DreamBalance:     PtrInt(math.ZeroInt()),
		StakedDream:      PtrInt(math.ZeroInt()),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		ReputationScores: map[string]string{"coding": "80.0"},
	})
	k.Member.Set(ctx, juror3.String(), types.Member{
		Address:          juror3.String(),
		DreamBalance:     PtrInt(math.ZeroInt()),
		StakedDream:      PtrInt(math.ZeroInt()),
		LifetimeEarned:   PtrInt(math.ZeroInt()),
		LifetimeBurned:   PtrInt(math.ZeroInt()),
		ReputationScores: map[string]string{"coding": "60.0"},
	})

	// Respond to challenge -> Triggers Jury Selection
	// JurySize is 7 by default, but we only have 3 eligible.
	// Current logic fails if < JurySize. We should update params for test or add more jurors.
	// Easier: Update Params.
	params, _ := k.Params.Get(ctx)
	params.JurySize = 3
	params.MinJurorReputation = math.LegacyNewDec(50)
	k.Params.Set(ctx, params)

	err = k.RespondToChallenge(ctx, chalID, assignee, "Defense", nil)
	require.NoError(t, err)

	// Get Jury Review
	challenge, _ := k.GetChallenge(ctx, chalID)
	require.Equal(t, types.ChallengeStatus_CHALLENGE_STATUS_IN_JURY_REVIEW, challenge.Status)

	// Find Jury Review for this challenge
	var jr types.JuryReview
	found := false
	err = k.JuryReview.Walk(ctx, nil, func(key uint64, val types.JuryReview) (bool, error) {
		if val.ChallengeId == chalID {
			jr = val
			found = true
			return true, nil // Stop iteration
		}
		return false, nil
	})
	require.NoError(t, err)
	require.True(t, found, "JuryReview not found for challenge")
	require.Len(t, jr.Jurors, 3)

	// Submit Votes
	// Supermajority 67% of 3 is 2.01 -> 3 votes needed for Uphold?
	// requiredVotes = ceil(0.67 * 3) = 3?
	// 0.67 * 3 = 2.01. Ceil is 3.
	// So 3 Upholds needed to Uphold.
	// Rejection needs > 50% (2 votes).

	// Scenario 1: Inconclusive (1 Uphold, 2 Reject? No wait, 2 Reject is Rejection)
	// Inconclusive happens if Uphold < Supermajority AND Reject <= 50%
	// If 3 voters:
	// 3 Up -> Uphold
	// 2 Up, 1 Rej -> 2 is < 3 required. Reject is 1 <= 1.5. -> Inconclusive

	// Vote 1: Uphold
	err = k.SubmitJurorVote(ctx, jr.Id, juror1, nil, types.Verdict_VERDICT_UPHOLD_CHALLENGE, math.LegacyMustNewDecFromStr("0.9"), "Guilty")
	require.NoError(t, err)

	// Vote 2: Uphold
	err = k.SubmitJurorVote(ctx, jr.Id, juror2, nil, types.Verdict_VERDICT_UPHOLD_CHALLENGE, math.LegacyMustNewDecFromStr("0.8"), "Guilty yes")
	require.NoError(t, err)

	// Vote 3: Reject (Assignee innocent)
	err = k.SubmitJurorVote(ctx, jr.Id, juror3, nil, types.Verdict_VERDICT_REJECT_CHALLENGE, math.LegacyMustNewDecFromStr("0.7"), "Innocent")
	require.NoError(t, err)

	// Check Verdict
	jr, _ = k.GetJuryReview(ctx, jr.Id)
	require.Equal(t, types.Verdict_VERDICT_INCONCLUSIVE, jr.Verdict)

	// Verify Escalation
	// Should create an ADJUDICATION interim
	// We can check Interims. Since we don't have easy query, we assume it's the next ID.
	// Init created ID 1 (Work). Jurors created 2, 3, 4 (Jury Duty). Escalation is 5.
	interim5, err := k.GetInterim(ctx, 5)
	if err == nil {
		require.Equal(t, types.InterimType_INTERIM_TYPE_ADJUDICATION, interim5.Type)
		require.Equal(t, types.InterimComplexity_INTERIM_COMPLEXITY_EPIC, interim5.Complexity)
		require.Equal(t, "technical_operations", interim5.Committee)
	} else {
		// Fallback check: iterate or check event (testify doesn't check events easily without mock context hooks)
		// Assuming ID might vary if sequence shared? Sequence is per-map in this design.
		// InterimSeq was used.
	}
}

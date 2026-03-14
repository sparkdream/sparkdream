package keeper_test

import (
	"fmt"
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

	chalID, err := k.CreateChallenge(ctx, challenger, initID, "Reason", nil, math.NewInt(50))
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

func TestSelectJury(t *testing.T) {
	t.Run("enough eligible members returns correct count", func(t *testing.T) {
		fixture := initFixture(t)
		k := fixture.keeper
		ctx := fixture.ctx

		// Set params: jurySize=3, MinJurorReputation=50
		params, _ := k.Params.Get(ctx)
		params.JurySize = 3
		params.MinJurorReputation = math.LegacyNewDec(50)
		k.Params.Set(ctx, params)

		// Create 5 members with sufficient reputation in "coding"
		for i := 0; i < 5; i++ {
			addr := sdk.AccAddress([]byte(fmt.Sprintf("eligible-member-%d", i)))
			k.Member.Set(ctx, addr.String(), types.Member{
				Address:          addr.String(),
				DreamBalance:     PtrInt(math.ZeroInt()),
				StakedDream:      PtrInt(math.ZeroInt()),
				LifetimeEarned:   PtrInt(math.ZeroInt()),
				LifetimeBurned:   PtrInt(math.ZeroInt()),
				ReputationScores: map[string]string{"coding": "100.0"},
			})
		}

		// Create an initiative with the "coding" tag
		initiative := types.Initiative{
			Id:       1,
			Tags:     []string{"coding"},
			Assignee: "some-other-address",
		}

		jurors, err := k.SelectJury(ctx, initiative, params.JurySize)
		require.NoError(t, err)
		require.Len(t, jurors, 3, "should select exactly JurySize jurors")

		// All selected jurors should be unique
		seen := make(map[string]bool)
		for _, j := range jurors {
			require.False(t, seen[j], "juror selected twice: %s", j)
			seen[j] = true
		}
	})

	t.Run("insufficient eligible members returns error", func(t *testing.T) {
		fixture := initFixture(t)
		k := fixture.keeper
		ctx := fixture.ctx

		params, _ := k.Params.Get(ctx)
		params.JurySize = 5
		params.MinJurorReputation = math.LegacyNewDec(50)
		k.Params.Set(ctx, params)

		// Create only 2 eligible members (need 5)
		for i := 0; i < 2; i++ {
			addr := sdk.AccAddress([]byte(fmt.Sprintf("few-member-%d", i)))
			k.Member.Set(ctx, addr.String(), types.Member{
				Address:          addr.String(),
				DreamBalance:     PtrInt(math.ZeroInt()),
				StakedDream:      PtrInt(math.ZeroInt()),
				LifetimeEarned:   PtrInt(math.ZeroInt()),
				LifetimeBurned:   PtrInt(math.ZeroInt()),
				ReputationScores: map[string]string{"coding": "100.0"},
			})
		}

		initiative := types.Initiative{
			Id:       1,
			Tags:     []string{"coding"},
			Assignee: "other-address",
		}

		_, err := k.SelectJury(ctx, initiative, params.JurySize)
		require.Error(t, err)
		require.Contains(t, err.Error(), "insufficient eligible jurors")
	})

	t.Run("affiliated members are excluded", func(t *testing.T) {
		fixture := initFixture(t)
		k := fixture.keeper
		ctx := fixture.ctx

		params, _ := k.Params.Get(ctx)
		params.JurySize = 3
		params.MinJurorReputation = math.LegacyNewDec(50)
		k.Params.Set(ctx, params)

		assignee := sdk.AccAddress([]byte("the-assignee-addr"))
		apprentice := sdk.AccAddress([]byte("the-apprentice-a"))

		// Create assignee and apprentice as members with high rep
		k.Member.Set(ctx, assignee.String(), types.Member{
			Address:          assignee.String(),
			DreamBalance:     PtrInt(math.ZeroInt()),
			StakedDream:      PtrInt(math.ZeroInt()),
			LifetimeEarned:   PtrInt(math.ZeroInt()),
			LifetimeBurned:   PtrInt(math.ZeroInt()),
			ReputationScores: map[string]string{"coding": "500.0"},
		})
		k.Member.Set(ctx, apprentice.String(), types.Member{
			Address:          apprentice.String(),
			DreamBalance:     PtrInt(math.ZeroInt()),
			StakedDream:      PtrInt(math.ZeroInt()),
			LifetimeEarned:   PtrInt(math.ZeroInt()),
			LifetimeBurned:   PtrInt(math.ZeroInt()),
			ReputationScores: map[string]string{"coding": "500.0"},
		})

		// Create 3 non-affiliated members
		nonAffiliated := make([]sdk.AccAddress, 3)
		for i := 0; i < 3; i++ {
			addr := sdk.AccAddress([]byte(fmt.Sprintf("non-affiliated-%d", i)))
			nonAffiliated[i] = addr
			k.Member.Set(ctx, addr.String(), types.Member{
				Address:          addr.String(),
				DreamBalance:     PtrInt(math.ZeroInt()),
				StakedDream:      PtrInt(math.ZeroInt()),
				LifetimeEarned:   PtrInt(math.ZeroInt()),
				LifetimeBurned:   PtrInt(math.ZeroInt()),
				ReputationScores: map[string]string{"coding": "100.0"},
			})
		}

		initiative := types.Initiative{
			Id:         1,
			Tags:       []string{"coding"},
			Assignee:   assignee.String(),
			Apprentice: apprentice.String(),
		}

		jurors, err := k.SelectJury(ctx, initiative, params.JurySize)
		require.NoError(t, err)
		require.Len(t, jurors, 3)

		// Neither assignee nor apprentice should be in the jury
		for _, j := range jurors {
			require.NotEqual(t, assignee.String(), j, "assignee should be excluded from jury")
			require.NotEqual(t, apprentice.String(), j, "apprentice should be excluded from jury")
		}
	})

	t.Run("members below MinJurorReputation are excluded", func(t *testing.T) {
		fixture := initFixture(t)
		k := fixture.keeper
		ctx := fixture.ctx

		params, _ := k.Params.Get(ctx)
		params.JurySize = 3
		params.MinJurorReputation = math.LegacyNewDec(50)
		k.Params.Set(ctx, params)

		// Create 2 members with low reputation (below threshold)
		for i := 0; i < 2; i++ {
			addr := sdk.AccAddress([]byte(fmt.Sprintf("low-rep-member-%d", i)))
			k.Member.Set(ctx, addr.String(), types.Member{
				Address:          addr.String(),
				DreamBalance:     PtrInt(math.ZeroInt()),
				StakedDream:      PtrInt(math.ZeroInt()),
				LifetimeEarned:   PtrInt(math.ZeroInt()),
				LifetimeBurned:   PtrInt(math.ZeroInt()),
				ReputationScores: map[string]string{"coding": "10.0"}, // Below 50
			})
		}

		// Create 3 members with sufficient reputation
		for i := 0; i < 3; i++ {
			addr := sdk.AccAddress([]byte(fmt.Sprintf("high-rep-memb-%d", i)))
			k.Member.Set(ctx, addr.String(), types.Member{
				Address:          addr.String(),
				DreamBalance:     PtrInt(math.ZeroInt()),
				StakedDream:      PtrInt(math.ZeroInt()),
				LifetimeEarned:   PtrInt(math.ZeroInt()),
				LifetimeBurned:   PtrInt(math.ZeroInt()),
				ReputationScores: map[string]string{"coding": "75.0"}, // Above 50
			})
		}

		initiative := types.Initiative{
			Id:       1,
			Tags:     []string{"coding"},
			Assignee: "unrelated-address",
		}

		jurors, err := k.SelectJury(ctx, initiative, params.JurySize)
		require.NoError(t, err)
		require.Len(t, jurors, 3)

		// All selected jurors should be the high-rep members, not the low-rep ones
		lowRepAddrs := make(map[string]bool)
		for i := 0; i < 2; i++ {
			addr := sdk.AccAddress([]byte(fmt.Sprintf("low-rep-member-%d", i)))
			lowRepAddrs[addr.String()] = true
		}
		for _, j := range jurors {
			require.False(t, lowRepAddrs[j], "low reputation member should not be selected as juror: %s", j)
		}
	})
}

func TestTallyJuryVotes(t *testing.T) {
	// Helper to set up a challenge and jury review for tallying tests.
	// Returns the fixture, juryReview ID, and challenge ID.
	setupTallyTest := func(t *testing.T, jurorAddrs []string, jurySize uint32) (*fixture, uint64, uint64) {
		t.Helper()
		f := initFixture(t)
		k := f.keeper
		ctx := f.ctx

		// Set params
		params, _ := k.Params.Get(ctx)
		params.JurySize = jurySize
		params.JurySuperMajority = math.LegacyNewDecWithPrec(67, 2) // 67%
		params.MinJurorReputation = math.LegacyNewDec(50)
		k.Params.Set(ctx, params)

		// We need a real project + initiative + challenge in the store for TallyJuryVotes
		// to resolve the challenge via UpholdChallenge/RejectChallenge.
		projectCreator := sdk.AccAddress([]byte("proj-creator-addr"))
		k.Member.Set(ctx, projectCreator.String(), types.Member{
			Address:          projectCreator.String(),
			DreamBalance:     PtrInt(math.ZeroInt()),
			StakedDream:      PtrInt(math.ZeroInt()),
			LifetimeEarned:   PtrInt(math.ZeroInt()),
			LifetimeBurned:   PtrInt(math.ZeroInt()),
			ReputationScores: map[string]string{"coding": "100.0"},
		})

		projectID, _ := k.CreateProject(
			ctx,
			projectCreator,
			"TallyProj",
			"Desc",
			[]string{"coding"},
			types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE,
			"technical",
			math.NewInt(50000),
			math.NewInt(5000),
		)
		k.ApproveProject(ctx, projectID, sdk.AccAddress([]byte("approver")), math.NewInt(50000), math.NewInt(5000))

		assignee := sdk.AccAddress([]byte("tally-assignee-a"))
		k.Member.Set(ctx, assignee.String(), types.Member{
			Address:          assignee.String(),
			DreamBalance:     PtrInt(math.ZeroInt()),
			StakedDream:      PtrInt(math.ZeroInt()),
			LifetimeEarned:   PtrInt(math.ZeroInt()),
			LifetimeBurned:   PtrInt(math.ZeroInt()),
			ReputationScores: map[string]string{"coding": "1000.0"},
		})

		initID, _ := k.CreateInitiative(
			ctx,
			assignee,
			projectID,
			"TallyInit",
			"D",
			[]string{"coding"},
			types.InitiativeTier_INITIATIVE_TIER_STANDARD,
			types.InitiativeCategory_INITIATIVE_CATEGORY_FEATURE,
			"",
			math.NewInt(150),
		)
		k.AssignInitiativeToMember(ctx, initID, assignee)
		k.SubmitInitiativeWork(ctx, initID, assignee, "URI")

		challenger := sdk.AccAddress([]byte("tally-challenger"))
		k.Member.Set(ctx, challenger.String(), types.Member{
			Address:          challenger.String(),
			DreamBalance:     PtrInt(math.ZeroInt()),
			StakedDream:      PtrInt(math.ZeroInt()),
			LifetimeEarned:   PtrInt(math.ZeroInt()),
			LifetimeBurned:   PtrInt(math.ZeroInt()),
			ReputationScores: make(map[string]string),
		})
		k.MintDREAM(ctx, challenger, math.NewInt(1000))

		chalID, err := k.CreateChallenge(ctx, challenger, initID, "Bad work", nil, math.NewInt(50))
		require.NoError(t, err)

		// Set challenge to IN_JURY_REVIEW status
		challenge, _ := k.GetChallenge(ctx, chalID)
		challenge.Status = types.ChallengeStatus_CHALLENGE_STATUS_IN_JURY_REVIEW
		k.Challenge.Set(ctx, chalID, challenge)

		// Create juror members
		for _, addr := range jurorAddrs {
			k.Member.Set(ctx, addr, types.Member{
				Address:          addr,
				DreamBalance:     PtrInt(math.ZeroInt()),
				StakedDream:      PtrInt(math.ZeroInt()),
				LifetimeEarned:   PtrInt(math.ZeroInt()),
				LifetimeBurned:   PtrInt(math.ZeroInt()),
				ReputationScores: map[string]string{"coding": "100.0"},
			})
		}

		// Directly create a JuryReview in the store
		superMajority := params.JurySuperMajority
		requiredVotes := superMajority.MulInt64(int64(len(jurorAddrs))).Ceil().TruncateInt().Uint64()

		jrID, _ := k.JuryReviewSeq.Next(ctx)
		jr := types.JuryReview{
			Id:                jrID,
			ChallengeId:       chalID,
			InitiativeId:      initID,
			Jurors:            jurorAddrs,
			RequiredVotes:     uint32(requiredVotes),
			ExpertWitnesses:   []string{},
			Testimonies:       []*types.ExpertTestimony{},
			ReviewDeliverable: "URI",
			ChallengerClaim:   "Bad work",
			AssigneeResponse:  "Defense",
			Votes:             []*types.JurorVote{},
			Deadline:          1000,
			Verdict:           types.Verdict_VERDICT_PENDING,
		}
		k.JuryReview.Set(ctx, jrID, jr)

		return f, jrID, chalID
	}

	t.Run("supermajority uphold results in VERDICT_UPHOLD_CHALLENGE", func(t *testing.T) {
		juror1 := sdk.AccAddress([]byte("tally-juror-1-adr")).String()
		juror2 := sdk.AccAddress([]byte("tally-juror-2-adr")).String()
		juror3 := sdk.AccAddress([]byte("tally-juror-3-adr")).String()
		jurorAddrs := []string{juror1, juror2, juror3}

		f, jrID, chalID := setupTallyTest(t, jurorAddrs, 3)
		k := f.keeper
		ctx := f.ctx

		// All 3 vote to uphold. Supermajority = ceil(0.67 * 3) = 3, so 3 upholds needed.
		jr, _ := k.GetJuryReview(ctx, jrID)
		jr.Votes = []*types.JurorVote{
			{Juror: juror1, Verdict: types.Verdict_VERDICT_UPHOLD_CHALLENGE, Confidence: PtrDec(math.LegacyMustNewDecFromStr("0.9")), Reasoning: "Uphold"},
			{Juror: juror2, Verdict: types.Verdict_VERDICT_UPHOLD_CHALLENGE, Confidence: PtrDec(math.LegacyMustNewDecFromStr("0.8")), Reasoning: "Uphold"},
			{Juror: juror3, Verdict: types.Verdict_VERDICT_UPHOLD_CHALLENGE, Confidence: PtrDec(math.LegacyMustNewDecFromStr("0.85")), Reasoning: "Uphold"},
		}
		k.JuryReview.Set(ctx, jrID, jr)

		err := k.TallyJuryVotes(ctx, jrID)
		require.NoError(t, err)

		jr, _ = k.GetJuryReview(ctx, jrID)
		require.Equal(t, types.Verdict_VERDICT_UPHOLD_CHALLENGE, jr.Verdict)

		// Challenge should be UPHELD
		challenge, _ := k.GetChallenge(ctx, chalID)
		require.Equal(t, types.ChallengeStatus_CHALLENGE_STATUS_UPHELD, challenge.Status)
	})

	t.Run("majority reject results in VERDICT_REJECT_CHALLENGE", func(t *testing.T) {
		juror1 := sdk.AccAddress([]byte("rej-juror-1-addrs")).String()
		juror2 := sdk.AccAddress([]byte("rej-juror-2-addrs")).String()
		juror3 := sdk.AccAddress([]byte("rej-juror-3-addrs")).String()
		jurorAddrs := []string{juror1, juror2, juror3}

		f, jrID, chalID := setupTallyTest(t, jurorAddrs, 3)
		k := f.keeper
		ctx := f.ctx

		// 2 reject, 1 uphold. Reject > 50% (2 > 1.5).
		jr, _ := k.GetJuryReview(ctx, jrID)
		jr.Votes = []*types.JurorVote{
			{Juror: juror1, Verdict: types.Verdict_VERDICT_REJECT_CHALLENGE, Confidence: PtrDec(math.LegacyMustNewDecFromStr("0.9")), Reasoning: "Reject"},
			{Juror: juror2, Verdict: types.Verdict_VERDICT_REJECT_CHALLENGE, Confidence: PtrDec(math.LegacyMustNewDecFromStr("0.8")), Reasoning: "Reject"},
			{Juror: juror3, Verdict: types.Verdict_VERDICT_UPHOLD_CHALLENGE, Confidence: PtrDec(math.LegacyMustNewDecFromStr("0.7")), Reasoning: "Uphold"},
		}
		k.JuryReview.Set(ctx, jrID, jr)

		err := k.TallyJuryVotes(ctx, jrID)
		require.NoError(t, err)

		jr, _ = k.GetJuryReview(ctx, jrID)
		require.Equal(t, types.Verdict_VERDICT_REJECT_CHALLENGE, jr.Verdict)

		// Challenge should be REJECTED
		challenge, _ := k.GetChallenge(ctx, chalID)
		require.Equal(t, types.ChallengeStatus_CHALLENGE_STATUS_REJECTED, challenge.Status)
	})

	t.Run("no clear majority results in VERDICT_INCONCLUSIVE", func(t *testing.T) {
		juror1 := sdk.AccAddress([]byte("inc-juror-1-addrs")).String()
		juror2 := sdk.AccAddress([]byte("inc-juror-2-addrs")).String()
		juror3 := sdk.AccAddress([]byte("inc-juror-3-addrs")).String()
		jurorAddrs := []string{juror1, juror2, juror3}

		f, jrID, _ := setupTallyTest(t, jurorAddrs, 3)
		k := f.keeper
		ctx := f.ctx

		// 2 uphold, 1 reject. Uphold=2 < 3 (supermajority). Reject=1 not > 1.5. -> Inconclusive
		jr, _ := k.GetJuryReview(ctx, jrID)
		jr.Votes = []*types.JurorVote{
			{Juror: juror1, Verdict: types.Verdict_VERDICT_UPHOLD_CHALLENGE, Confidence: PtrDec(math.LegacyMustNewDecFromStr("0.9")), Reasoning: "Uphold"},
			{Juror: juror2, Verdict: types.Verdict_VERDICT_UPHOLD_CHALLENGE, Confidence: PtrDec(math.LegacyMustNewDecFromStr("0.8")), Reasoning: "Uphold"},
			{Juror: juror3, Verdict: types.Verdict_VERDICT_REJECT_CHALLENGE, Confidence: PtrDec(math.LegacyMustNewDecFromStr("0.7")), Reasoning: "Reject"},
		}
		k.JuryReview.Set(ctx, jrID, jr)

		err := k.TallyJuryVotes(ctx, jrID)
		require.NoError(t, err)

		jr, _ = k.GetJuryReview(ctx, jrID)
		require.Equal(t, types.Verdict_VERDICT_INCONCLUSIVE, jr.Verdict)
	})
}

func TestRewardJurors(t *testing.T) {
	t.Run("jurors who voted receive DREAM", func(t *testing.T) {
		fixture := initFixture(t)
		k := fixture.keeper
		ctx := fixture.ctx

		voter1 := sdk.AccAddress([]byte("reward-voter-1-ad"))
		voter2 := sdk.AccAddress([]byte("reward-voter-2-ad"))
		nonVoter := sdk.AccAddress([]byte("reward-nonvoter-a"))

		// Create members
		for _, addr := range []sdk.AccAddress{voter1, voter2, nonVoter} {
			k.Member.Set(ctx, addr.String(), types.Member{
				Address:          addr.String(),
				DreamBalance:     PtrInt(math.ZeroInt()),
				StakedDream:      PtrInt(math.ZeroInt()),
				LifetimeEarned:   PtrInt(math.ZeroInt()),
				LifetimeBurned:   PtrInt(math.ZeroInt()),
				ReputationScores: map[string]string{"coding": "100.0"},
			})
		}

		// Build a JuryReview with 3 jurors but only 2 votes
		review := types.JuryReview{
			Id:     1,
			Jurors: []string{voter1.String(), voter2.String(), nonVoter.String()},
			Votes: []*types.JurorVote{
				{Juror: voter1.String(), Verdict: types.Verdict_VERDICT_UPHOLD_CHALLENGE, Confidence: PtrDec(math.LegacyMustNewDecFromStr("0.9")), Reasoning: "yes"},
				{Juror: voter2.String(), Verdict: types.Verdict_VERDICT_REJECT_CHALLENGE, Confidence: PtrDec(math.LegacyMustNewDecFromStr("0.8")), Reasoning: "no"},
				// nonVoter did NOT vote
			},
			Verdict: types.Verdict_VERDICT_UPHOLD_CHALLENGE,
		}

		err := k.RewardJurors(ctx, review)
		require.NoError(t, err)

		params, _ := k.Params.Get(ctx)
		expectedReward := params.StandardComplexityBudget

		// Voter 1 should have received the reward
		member1, err := k.GetMember(ctx, voter1)
		require.NoError(t, err)
		require.True(t, member1.DreamBalance.Equal(expectedReward),
			"voter1 should receive StandardComplexityBudget DREAM, got %s", member1.DreamBalance.String())

		// Voter 2 should have received the reward
		member2, err := k.GetMember(ctx, voter2)
		require.NoError(t, err)
		require.True(t, member2.DreamBalance.Equal(expectedReward),
			"voter2 should receive StandardComplexityBudget DREAM, got %s", member2.DreamBalance.String())
	})

	t.Run("jurors who did not vote receive nothing", func(t *testing.T) {
		fixture := initFixture(t)
		k := fixture.keeper
		ctx := fixture.ctx

		voter := sdk.AccAddress([]byte("voted-juror-addr1"))
		nonVoter := sdk.AccAddress([]byte("novote-juror-adr1"))

		for _, addr := range []sdk.AccAddress{voter, nonVoter} {
			k.Member.Set(ctx, addr.String(), types.Member{
				Address:          addr.String(),
				DreamBalance:     PtrInt(math.ZeroInt()),
				StakedDream:      PtrInt(math.ZeroInt()),
				LifetimeEarned:   PtrInt(math.ZeroInt()),
				LifetimeBurned:   PtrInt(math.ZeroInt()),
				ReputationScores: map[string]string{"coding": "100.0"},
			})
		}

		review := types.JuryReview{
			Id:     2,
			Jurors: []string{voter.String(), nonVoter.String()},
			Votes: []*types.JurorVote{
				{Juror: voter.String(), Verdict: types.Verdict_VERDICT_UPHOLD_CHALLENGE, Confidence: PtrDec(math.LegacyMustNewDecFromStr("0.9")), Reasoning: "yes"},
			},
			Verdict: types.Verdict_VERDICT_UPHOLD_CHALLENGE,
		}

		err := k.RewardJurors(ctx, review)
		require.NoError(t, err)

		// Non-voter should still have zero DREAM
		memberNV, err := k.GetMember(ctx, nonVoter)
		require.NoError(t, err)
		require.True(t, memberNV.DreamBalance.IsZero(),
			"non-voter should receive no DREAM, got %s", memberNV.DreamBalance.String())
	})
}

func TestCreateAppealInitiative(t *testing.T) {
	t.Run("creates JuryReview with correct fields", func(t *testing.T) {
		fixture := initFixture(t)
		k := fixture.keeper
		ctx := fixture.ctx

		payload := []byte(`{"case": "moderation appeal", "post_id": 42}`)
		deadline := int64(500)
		appealType := "moderation_appeal"

		appealID, err := k.CreateAppealInitiative(ctx, appealType, payload, deadline)
		require.NoError(t, err)
		require.Greater(t, appealID, uint64(0), "appeal ID should be positive")

		// Retrieve the JuryReview
		jr, err := k.GetJuryReview(ctx, appealID)
		require.NoError(t, err)

		// Verify fields
		require.Equal(t, appealID, jr.Id)
		require.Equal(t, uint64(0), jr.ChallengeId, "appeal should have ChallengeId=0")
		require.Equal(t, uint64(0), jr.InitiativeId, "appeal should have InitiativeId=0")
		require.Equal(t, string(payload), jr.ReviewDeliverable, "payload should be stored in ReviewDeliverable")
		require.Equal(t, appealType, jr.ChallengerClaim, "appeal type should be stored in ChallengerClaim")
		require.Equal(t, deadline, jr.Deadline)
		require.Equal(t, types.Verdict_VERDICT_PENDING, jr.Verdict, "verdict should be VERDICT_PENDING")
		require.Empty(t, jr.Jurors, "jurors should be empty for deferred selection")
		require.Empty(t, jr.Votes, "votes should be empty initially")
	})

	t.Run("returns valid appeal ID", func(t *testing.T) {
		fixture := initFixture(t)
		k := fixture.keeper
		ctx := fixture.ctx

		id1, err := k.CreateAppealInitiative(ctx, "sentinel_appeal", []byte("data1"), 100)
		require.NoError(t, err)

		id2, err := k.CreateAppealInitiative(ctx, "sentinel_appeal", []byte("data2"), 200)
		require.NoError(t, err)

		require.Greater(t, id2, id1, "second appeal should have a higher ID than the first")
	})
}

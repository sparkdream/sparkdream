package keeper_test

import (
	"strings"
	"testing"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

// Acceptance criteria arm the challenger — the only actor in the initiative
// lifecycle who stakes something that burns if they are wrong — with an
// objective standard the author agreed to before starting. They are
// deliberately not a completion gate of their own; see acceptance_criteria.go.

func criteriaFixture(t *testing.T, criteria ...types.VerificationCriteria) (*fixture, keeper.Keeper, sdk.Context, sdk.AccAddress, uint64) {
	t.Helper()
	f := initFixture(t)
	k, ctx := f.keeper, f.ctx

	creator := sdk.AccAddress([]byte("ac-creator-addr--"))
	mkFundedMember(t, k, ctx, creator, 10_000_000)
	projectID, err := k.CreateProject(ctx, creator, "Proj", "Desc", []string{"tag"},
		types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "technical",
		math.NewInt(10000), math.NewInt(1000), false)
	require.NoError(t, err)
	require.NoError(t, k.ApproveProject(ctx, projectID, sdk.AccAddress([]byte("approver")),
		math.NewInt(10000), math.NewInt(1000)))

	initID, err := k.CreateInitiative(ctx, creator, projectID, "Task", "D", []string{"tag"},
		types.InitiativeTier_INITIATIVE_TIER_STANDARD,
		types.InitiativeCategory_INITIATIVE_CATEGORY_FEATURE, math.NewInt(1000), criteria...)
	require.NoError(t, err)
	return f, k, ctx, creator, initID
}

func TestAcceptanceCriteriaArePersistedAtCreation(t *testing.T) {
	_, k, ctx, _, initID := criteriaFixture(t,
		types.VerificationCriteria{Id: "builds", Question: "Does it build?", Required: true},
		types.VerificationCriteria{Id: "docs", Question: "Are the docs updated?"},
	)

	initiative, err := k.GetInitiative(ctx, initID)
	require.NoError(t, err)
	require.Len(t, initiative.AcceptanceCriteria, 2)
	require.Equal(t, "builds", initiative.AcceptanceCriteria[0].Id)
	require.True(t, initiative.AcceptanceCriteria[0].Required)
}

func TestAcceptanceCriteriaValidation(t *testing.T) {
	tests := []struct {
		name     string
		criteria []types.VerificationCriteria
		wantErr  error
	}{
		{"none is fine", nil, nil},
		{
			"empty id",
			[]types.VerificationCriteria{{Question: "Does it build?"}},
			types.ErrInvalidAcceptanceCriteria,
		},
		{
			// Duplicates make a CriteriaVote ambiguous about what it answered.
			"duplicate id",
			[]types.VerificationCriteria{
				{Id: "builds", Question: "Does it build?"},
				{Id: "builds", Question: "Really?"},
			},
			types.ErrInvalidAcceptanceCriteria,
		},
		{
			"missing question",
			[]types.VerificationCriteria{{Id: "builds"}},
			types.ErrInvalidAcceptanceCriteria,
		},
		{
			"question too long",
			[]types.VerificationCriteria{{Id: "builds", Question: strings.Repeat("x", types.MaxCriteriaQuestionLength+1)}},
			types.ErrInvalidAcceptanceCriteria,
		},
		{
			"id too long",
			[]types.VerificationCriteria{{Id: strings.Repeat("x", types.MaxCriteriaIDLength+1), Question: "Q"}},
			types.ErrInvalidAcceptanceCriteria,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := keeper.ValidateAcceptanceCriteria(tc.criteria)
			if tc.wantErr == nil {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, tc.wantErr)
		})
	}

	t.Run("too many criteria", func(t *testing.T) {
		many := make([]types.VerificationCriteria, types.MaxAcceptanceCriteria+1)
		for i := range many {
			many[i] = types.VerificationCriteria{Id: string(rune('a'+i%26)) + string(rune('0'+i/26)), Question: "Q"}
		}
		require.ErrorIs(t, keeper.ValidateAcceptanceCriteria(many), types.ErrInvalidAcceptanceCriteria)
	})
}

func TestCreateInitiativeRejectsBadCriteria(t *testing.T) {
	f := initFixture(t)
	k, ctx := f.keeper, f.ctx

	creator := sdk.AccAddress([]byte("ac-bad-creator---"))
	mkFundedMember(t, k, ctx, creator, 10_000_000)
	projectID, err := k.CreateProject(ctx, creator, "Proj", "Desc", []string{"tag"},
		types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "technical",
		math.NewInt(10000), math.NewInt(1000), false)
	require.NoError(t, err)
	require.NoError(t, k.ApproveProject(ctx, projectID, sdk.AccAddress([]byte("approver")),
		math.NewInt(10000), math.NewInt(1000)))

	_, err = k.CreateInitiative(ctx, creator, projectID, "Task", "D", []string{"tag"},
		types.InitiativeTier_INITIATIVE_TIER_STANDARD,
		types.InitiativeCategory_INITIATIVE_CATEGORY_FEATURE, math.NewInt(1000),
		types.VerificationCriteria{Id: "", Question: "Does it build?"})
	require.ErrorIs(t, err, types.ErrInvalidAcceptanceCriteria)
}

func TestChallengeCitesADeclaredCriterion(t *testing.T) {
	_, k, ctx, creator, initID := criteriaFixture(t,
		types.VerificationCriteria{Id: "builds", Question: "Does it build?", Required: true})

	// Move the initiative into a challengeable state.
	initiative, err := k.GetInitiative(ctx, initID)
	require.NoError(t, err)
	initiative.Assignee = creator.String()
	initiative.Status = types.InitiativeStatus_INITIATIVE_STATUS_SUBMITTED
	require.NoError(t, k.UpdateInitiative(ctx, initiative))

	challenger := sdk.AccAddress([]byte("ac-challenger----"))
	mkFundedMember(t, k, ctx, challenger, 0)
	require.NoError(t, k.MintDREAM(ctx, challenger, math.NewInt(1_000_000_000)))

	t.Run("an undeclared criterion is rejected before the stake is locked", func(t *testing.T) {
		before, err := k.GetMember(ctx, challenger)
		require.NoError(t, err)

		_, err = k.CreateChallenge(ctx, challenger, initID, "broken", nil,
			math.NewInt(50_000_000), "no-such-criterion")
		require.ErrorIs(t, err, types.ErrUnknownAcceptanceCriterion)

		after, err := k.GetMember(ctx, challenger)
		require.NoError(t, err)
		require.Equal(t, before.StakedDream.String(), after.StakedDream.String(),
			"a typo must not cost the challenger their stake")
	})

	t.Run("a declared criterion is recorded on the challenge", func(t *testing.T) {
		chalID, err := k.CreateChallenge(ctx, challenger, initID, "does not compile", nil,
			math.NewInt(50_000_000), "builds")
		require.NoError(t, err)

		challenge, err := k.GetChallenge(ctx, chalID)
		require.NoError(t, err)
		require.Equal(t, "builds", challenge.CriteriaId)
	})
}

func TestChallengeWithoutCitationStillAllowed(t *testing.T) {
	// Criteria are optional and so is citing one — an initiative that declared
	// none can only ever be challenged free-form, and that path must keep working.
	_, k, ctx, creator, initID := criteriaFixture(t)

	initiative, err := k.GetInitiative(ctx, initID)
	require.NoError(t, err)
	initiative.Assignee = creator.String()
	initiative.Status = types.InitiativeStatus_INITIATIVE_STATUS_SUBMITTED
	require.NoError(t, k.UpdateInitiative(ctx, initiative))

	challenger := sdk.AccAddress([]byte("ac-freeform-chal-"))
	mkFundedMember(t, k, ctx, challenger, 0)
	require.NoError(t, k.MintDREAM(ctx, challenger, math.NewInt(1_000_000_000)))

	// Citing a criterion against an initiative that declared none is an error,
	// not a silently ignored field. Checked first: a successful challenge moves
	// the initiative to CHALLENGED, after which the status guard would preempt
	// criteria validation and this assertion would pass for the wrong reason.
	_, err = k.CreateChallenge(ctx, challenger, initID, "sloppy", nil,
		math.NewInt(50_000_000), "invented")
	require.ErrorIs(t, err, types.ErrUnknownAcceptanceCriterion)

	chalID, err := k.CreateChallenge(ctx, challenger, initID, "sloppy", nil, math.NewInt(50_000_000))
	require.NoError(t, err)
	challenge, err := k.GetChallenge(ctx, chalID)
	require.NoError(t, err)
	require.Empty(t, challenge.CriteriaId)
}

func TestCriteriaVotesResolveAgainstTheInitiative(t *testing.T) {
	initiative := types.Initiative{
		Id: 7,
		AcceptanceCriteria: []types.VerificationCriteria{
			{Id: "builds", Question: "Does it build?"},
			{Id: "docs", Question: "Docs updated?"},
		},
	}

	require.NoError(t, keeper.ValidateCriteriaVotes(initiative, []*types.CriteriaVote{
		{CriteriaId: "builds", Passed: true},
		{CriteriaId: "docs", Passed: false},
	}))

	require.ErrorIs(t,
		keeper.ValidateCriteriaVotes(initiative, []*types.CriteriaVote{{CriteriaId: "vibes"}}),
		types.ErrUnknownAcceptanceCriterion,
		"an id that resolves to nothing is what made CriteriaVote decorative")

	require.ErrorIs(t,
		keeper.ValidateCriteriaVotes(initiative, []*types.CriteriaVote{
			{CriteriaId: "builds"}, {CriteriaId: "builds"},
		}),
		types.ErrInvalidAcceptanceCriteria)

	require.ErrorIs(t,
		keeper.ValidateCriteriaVotes(types.Initiative{Id: 8}, []*types.CriteriaVote{{CriteriaId: "builds"}}),
		types.ErrUnknownAcceptanceCriterion)

	require.NoError(t, keeper.ValidateCriteriaVotes(types.Initiative{Id: 8}, nil),
		"no votes on an initiative with no criteria is the ordinary case")
}

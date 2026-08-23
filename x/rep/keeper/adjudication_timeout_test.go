package keeper_test

import (
	"testing"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

// An inconclusive jury used to be a permanent freeze. TallyJuryVotes raised an
// ADJUDICATION interim for the committee and left the challenge in
// IN_JURY_REVIEW; HasActiveChallenges counts that as active, so
// CanCompleteInitiative never returned true again. If the committee never acted,
// ExpireInterim marked the interim EXPIRED and touched nothing — leaving the
// initiative, the challenger's stake, every staker's DREAM and the assignee's
// self-assign bond locked indefinitely.

// challengedInitiative sets up an initiative with an unresolved challenge
// sitting in IN_JURY_REVIEW — the state an inconclusive jury leaves behind.
func challengedInitiative(t *testing.T, k keeper.Keeper, ctx sdk.Context) (uint64, uint64, sdk.AccAddress) {
	t.Helper()

	creator := sdk.AccAddress([]byte("adj-creator-addr-"))
	mkFundedMember(t, k, ctx, creator, 10_000_000)
	projectID, err := k.CreateProject(ctx, creator, "Proj", "Desc", []string{"tag"},
		types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "technical",
		math.NewInt(10000), math.NewInt(1000), false)
	require.NoError(t, err)
	require.NoError(t, k.ApproveProject(ctx, projectID, sdk.AccAddress([]byte("approver")),
		math.NewInt(10000), math.NewInt(1000)))

	initID, err := k.CreateInitiative(ctx, creator, projectID, "Task", "D", []string{"tag"},
		types.InitiativeTier_INITIATIVE_TIER_STANDARD,
		types.InitiativeCategory_INITIATIVE_CATEGORY_FEATURE, math.NewInt(1000))
	require.NoError(t, err)

	initiative, err := k.GetInitiative(ctx, initID)
	require.NoError(t, err)
	initiative.Assignee = creator.String()
	initiative.Status = types.InitiativeStatus_INITIATIVE_STATUS_SUBMITTED
	require.NoError(t, k.UpdateInitiative(ctx, initiative))

	challenger := sdk.AccAddress([]byte("adj-challenger---"))
	mkFundedMember(t, k, ctx, challenger, 0)
	require.NoError(t, k.MintDREAM(ctx, challenger, math.NewInt(1_000_000_000)))

	chalID, err := k.CreateChallenge(ctx, challenger, initID, "disputed", nil, math.NewInt(50_000_000))
	require.NoError(t, err)

	// Put the challenge where an inconclusive jury leaves it.
	challenge, err := k.GetChallenge(ctx, chalID)
	require.NoError(t, err)
	oldStatus := challenge.Status
	challenge.Status = types.ChallengeStatus_CHALLENGE_STATUS_IN_JURY_REVIEW
	require.NoError(t, k.Challenge.Set(ctx, chalID, challenge))
	require.NoError(t, k.UpdateChallengeStatusIndex(ctx, oldStatus, challenge.Status, chalID))

	return initID, chalID, challenger
}

func TestAdjudicationTimeoutResolvesTheChallenge(t *testing.T) {
	f := initFixture(t)
	k, ctx := f.keeper, f.ctx

	initID, chalID, challenger := challengedInitiative(t, k, ctx)

	// The initiative is frozen while the challenge is unresolved.
	blocked, err := k.HasActiveChallenges(ctx, initID)
	require.NoError(t, err)
	require.True(t, blocked)

	interimID, err := k.CreateInterimWork(ctx,
		types.InterimType_INTERIM_TYPE_ADJUDICATION,
		[]string{k.GetAuthorityString()},
		"technical_operations",
		initID,
		"Inconclusive jury",
		types.InterimComplexity_INTERIM_COMPLEXITY_EPIC,
		ctx.BlockHeight()+10)
	require.NoError(t, err)

	before, err := k.GetMember(ctx, challenger)
	require.NoError(t, err)

	require.NoError(t, k.ExpireInterim(ctx, interimID))

	// Default is REJECT: the work stands, and the challenger's stake burns so
	// that engineering a stalled jury is not free.
	challenge, err := k.GetChallenge(ctx, chalID)
	require.NoError(t, err)
	require.Equal(t, types.ChallengeStatus_CHALLENGE_STATUS_REJECTED, challenge.Status)

	after, err := k.GetMember(ctx, challenger)
	require.NoError(t, err)
	require.True(t, after.DreamBalance.LT(*before.DreamBalance), "challenger stake is burned")
	require.True(t, after.StakedDream.IsZero(), "and unlocked first, so staked does not overstate")

	// And the initiative is unfrozen.
	blocked, err = k.HasActiveChallenges(ctx, initID)
	require.NoError(t, err)
	require.False(t, blocked, "the initiative must be completable again")

	initiative, err := k.GetInitiative(ctx, initID)
	require.NoError(t, err)
	require.Equal(t, types.InitiativeStatus_INITIATIVE_STATUS_IN_REVIEW, initiative.Status)
}

func TestAdjudicationTimeoutLeavesResolvedChallengesAlone(t *testing.T) {
	f := initFixture(t)
	k, ctx := f.keeper, f.ctx

	initID, chalID, _ := challengedInitiative(t, k, ctx)

	// Committee resolved it before the deadline; the expiry must not re-resolve
	// a challenge that already has a verdict.
	require.NoError(t, k.UpholdChallenge(ctx, chalID))
	resolved, err := k.GetChallenge(ctx, chalID)
	require.NoError(t, err)
	require.Equal(t, types.ChallengeStatus_CHALLENGE_STATUS_UPHELD, resolved.Status)

	interimID, err := k.CreateInterimWork(ctx,
		types.InterimType_INTERIM_TYPE_ADJUDICATION,
		[]string{k.GetAuthorityString()},
		"technical_operations", initID, "Inconclusive jury",
		types.InterimComplexity_INTERIM_COMPLEXITY_EPIC, ctx.BlockHeight()+10)
	require.NoError(t, err)

	require.NoError(t, k.ExpireInterim(ctx, interimID))

	still, err := k.GetChallenge(ctx, chalID)
	require.NoError(t, err)
	require.Equal(t, types.ChallengeStatus_CHALLENGE_STATUS_UPHELD, still.Status,
		"an already-resolved challenge keeps its verdict")
}

func TestNonAdjudicationInterimExpiryIsUnaffected(t *testing.T) {
	f := initFixture(t)
	k, ctx := f.keeper, f.ctx

	initID, chalID, _ := challengedInitiative(t, k, ctx)

	// An ordinary interim referencing the same initiative must not resolve
	// anything — only ADJUDICATION carries the timeout verdict.
	assignee := sdk.AccAddress([]byte("adj-worker-addr--"))
	mkFundedMember(t, k, ctx, assignee, 10_000_000)
	interimID, err := k.CreateInterimWork(ctx,
		types.InterimType_INTERIM_TYPE_AUDIT,
		[]string{assignee.String()},
		"technical_operations", initID, "Unrelated work",
		types.InterimComplexity_INTERIM_COMPLEXITY_SIMPLE, ctx.BlockHeight()+10)
	require.NoError(t, err)

	require.NoError(t, k.ExpireInterim(ctx, interimID))

	challenge, err := k.GetChallenge(ctx, chalID)
	require.NoError(t, err)
	require.Equal(t, types.ChallengeStatus_CHALLENGE_STATUS_IN_JURY_REVIEW, challenge.Status)
}

// Derived indexes are not exported in genesis, so InitGenesis has to rebuild
// them. Only the project index did. The challenge one matters most:
// HasActiveChallenges reads it and CanCompleteInitiative reads that, so an
// unpopulated index reports "no active challenges" and a challenged initiative
// could pay out after a restart.
func TestGenesisRebuildsDerivedIndexes(t *testing.T) {
	f := initFixture(t)
	k, ctx := f.keeper, f.ctx

	initID, chalID, _ := challengedInitiative(t, k, ctx)
	juror := sdk.AccAddress([]byte("gen-juror-address")).String()
	review := types.JuryReview{Id: 1, InitiativeId: initID, Jurors: []string{juror},
		Verdict: types.Verdict_VERDICT_PENDING}
	require.NoError(t, k.JuryReview.Set(ctx, review.Id, review))

	exported, err := k.ExportGenesis(ctx)
	require.NoError(t, err)

	// Fresh state, same genesis.
	f2 := initFixture(t)
	k2, ctx2 := f2.keeper, f2.ctx
	require.NoError(t, k2.InitGenesis(ctx2, *exported))

	blocked, err := k2.HasActiveChallenges(ctx2, initID)
	require.NoError(t, err)
	require.True(t, blocked,
		"an unresolved challenge must still block completion after a genesis restart")

	found := false
	require.NoError(t, k2.IterateChallengesByStatus(ctx2,
		types.ChallengeStatus_CHALLENGE_STATUS_IN_JURY_REVIEW,
		func(id uint64, _ types.Challenge) bool {
			if id == chalID {
				found = true
			}
			return false
		}))
	require.True(t, found, "challenge status index rebuilt")

	pending := false
	require.NoError(t, k2.IterateJuryReviewsByVerdict(ctx2, types.Verdict_VERDICT_PENDING,
		func(id uint64, _ types.JuryReview) bool {
			if id == review.Id {
				pending = true
			}
			return false
		}))
	require.True(t, pending, "jury verdict index rebuilt, so the deadline sweep still sees it")

	seated := false
	require.NoError(t, k2.IterateJuryReviewsByJuror(ctx2, juror, func(id uint64, _ types.JuryReview) bool {
		if id == review.Id {
			seated = true
		}
		return false
	}))
	require.True(t, seated, "by-juror index rebuilt, so a juror keeps finding their summons")

	statusIndexed := false
	require.NoError(t, k2.IterateInitiativesByStatuses(ctx2,
		[]types.InitiativeStatus{types.InitiativeStatus_INITIATIVE_STATUS_CHALLENGED},
		func(id uint64, _ types.Initiative) bool {
			if id == initID {
				statusIndexed = true
			}
			return false
		}))
	require.True(t, statusIndexed, "initiative status index rebuilt")
}

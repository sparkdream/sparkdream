package keeper_test

import (
	"testing"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"

	errorsmod "cosmossdk.io/errors"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

// Releasing an assignment must not point the next submission at the previous
// holder's verdicts. They are keyed (initiative, round, reviewer) and outlive
// the assignment, so rewinding the round counter would let a stale approval
// satisfy the review gate for work nobody looked at, draw a second review fee,
// and lock its author out of filing a real verdict.
func TestUnassignDoesNotCarryVerdictsIntoTheNextRound(t *testing.T) {
	rf := setupReview(t, 1)
	k, ctx := rf.f.keeper, rf.f.ctx

	require.NoError(t, k.SubmitInitiativeReview(ctx, rf.initiative, rf.reviewer.String(), true, nil, "looks fine"))

	before, err := k.GetInitiative(ctx, rf.initiative)
	require.NoError(t, err)

	// The committee frees work that has stalled mid-review.
	require.NoError(t, k.UnassignInitiative(ctx, rf.initiative, "stalled", true))

	released, err := k.GetInitiative(ctx, rf.initiative)
	require.NoError(t, err)
	require.Equal(t, types.InitiativeStatus_INITIATIVE_STATUS_OPEN, released.Status)
	require.Greater(t, released.ReviewRound, before.ReviewRound,
		"a round that collected verdicts is advanced past, never rewound")

	// Somebody else picks the work up and submits their own deliverable.
	next := sdk.AccAddress([]byte("rv-assignee2-----"))
	mkReviewMember(t, k, ctx, next, "500.0")
	require.NoError(t, k.AssignInitiativeToMember(ctx, rf.initiative, next))
	require.NoError(t, k.SubmitInitiativeWork(ctx, rf.initiative, next, "ipfs://other-work"))

	initiative, err := k.GetInitiative(ctx, rf.initiative)
	require.NoError(t, err)
	project, err := k.GetProject(ctx, initiative.ProjectId)
	require.NoError(t, err)

	fresh, err := k.GetInitiativeReviews(ctx, rf.initiative, initiative.ReviewRound)
	require.NoError(t, err)
	require.Empty(t, fresh, "the new submission starts with no verdicts on it")

	satisfied, err := k.ReviewGateSatisfied(ctx, gateParams(t, k, ctx), initiative, project)
	require.NoError(t, err)
	require.False(t, satisfied, "the gate is not satisfied by a verdict on withdrawn work")

	minted, err := k.PayReviewFees(ctx, initiative)
	require.NoError(t, err)
	require.True(t, minted.IsZero(), "the discarded round's reviewer is not paid a second time")

	// And the reviewer who saw the first submission can still judge this one.
	require.NoError(t, k.SubmitInitiativeReview(ctx, rf.initiative, rf.reviewer.String(), false, nil, "not this time"))
}

// A release that discards nothing costs nothing. Self-assigning and stepping
// back down is free apart from gas, so consuming a round there would let anyone
// burn an initiative's max_review_rounds budget from the outside.
func TestUnassignWithNoVerdictsLeavesTheRoundAlone(t *testing.T) {
	rf := setupReview(t, 1)
	k, ctx := rf.f.keeper, rf.f.ctx

	before, err := k.GetInitiative(ctx, rf.initiative)
	require.NoError(t, err)

	require.NoError(t, k.UnassignInitiative(ctx, rf.initiative, "nobody reviewed it", true))

	after, err := k.GetInitiative(ctx, rf.initiative)
	require.NoError(t, err)
	require.Equal(t, before.ReviewRound, after.ReviewRound)
}

// Closing an initiative must take its escalation entry with it.
// resolveSilentEscalations walks that keyset rather than the status index, so
// an entry left behind times out later and hands rejectReviewRound a dead
// initiative — which would put it back to ASSIGNED with its budget already
// returned to the project and its self-assign bond already released.
func TestCloseClearsEscalationSoTheSweepCannotResurrectIt(t *testing.T) {
	rf := setupReview(t, 1)
	k, ctx := rf.f.keeper, rf.f.ctx

	submitted, err := k.GetInitiative(ctx, rf.initiative)
	require.NoError(t, err)

	// Nobody reviews; the deadline passes and the round goes to the committee.
	ctx = ctx.WithBlockHeight(submitted.ReviewDeadline + 1)
	require.NoError(t, k.SweepReviewDeadlines(ctx))
	escalated, err := k.EscalatedReviews.Has(ctx, rf.initiative)
	require.NoError(t, err)
	require.True(t, escalated)

	require.NoError(t, k.CloseInitiative(ctx, rf.initiative, "project pivoted"))
	escalated, err = k.EscalatedReviews.Has(ctx, rf.initiative)
	require.NoError(t, err)
	require.False(t, escalated, "a terminal initiative is no longer with the committee")

	// The committee's own window lapses. The sweep must find nothing to do.
	closed, err := k.GetInitiative(ctx, rf.initiative)
	require.NoError(t, err)
	ctx = ctx.WithBlockHeight(closed.ReviewDeadline + 1)
	require.NoError(t, k.SweepReviewDeadlines(ctx))

	final, err := k.GetInitiative(ctx, rf.initiative)
	require.NoError(t, err)
	require.Equal(t, types.InitiativeStatus_INITIATIVE_STATUS_CLOSED, final.Status,
		"a closed initiative stays closed")
}

// The cancel cascade is the other path to a terminal status while a round is
// with the committee, and carries the same hazard.
func TestProjectCancelClearsEscalation(t *testing.T) {
	rf := setupReview(t, 1)
	k, ctx := rf.f.keeper, rf.f.ctx

	submitted, err := k.GetInitiative(ctx, rf.initiative)
	require.NoError(t, err)
	ctx = ctx.WithBlockHeight(submitted.ReviewDeadline + 1)
	require.NoError(t, k.SweepReviewDeadlines(ctx))
	escalated, err := k.EscalatedReviews.Has(ctx, rf.initiative)
	require.NoError(t, err)
	require.True(t, escalated)

	require.NoError(t, k.CancelProject(ctx, rf.projectID, "winding down"))

	escalated, err = k.EscalatedReviews.Has(ctx, rf.initiative)
	require.NoError(t, err)
	require.False(t, escalated)

	closed, err := k.GetInitiative(ctx, rf.initiative)
	require.NoError(t, err)
	ctx = ctx.WithBlockHeight(closed.ReviewDeadline + 1)
	require.NoError(t, k.SweepReviewDeadlines(ctx))

	final, err := k.GetInitiative(ctx, rf.initiative)
	require.NoError(t, err)
	require.Equal(t, types.InitiativeStatus_INITIATIVE_STATUS_CLOSED, final.Status)
}

// Every status gate on the two exit paths carries a registered error, so a
// client sees an ABCI code rather than the generic code 1 an untyped
// fmt.Errorf collapses to. The split between the two codes is the point:
// ErrUnauthorized means the same call from another signer would work,
// ErrInvalidInitiativeStatus means no signer can do it from here.
func TestReleaseAndCloseGatesCarryRegisteredErrors(t *testing.T) {
	for _, tc := range []struct {
		name    string
		status  types.InitiativeStatus
		release bool // true: UnassignInitiative, false: CloseInitiative
		forced  bool
		wantErr *errorsmod.Error
	}{
		{"release from SUBMITTED unforced is an authorization failure",
			types.InitiativeStatus_INITIATIVE_STATUS_SUBMITTED, true, false, types.ErrUnauthorized},
		{"release from IN_REVIEW unforced is an authorization failure",
			types.InitiativeStatus_INITIATIVE_STATUS_IN_REVIEW, true, false, types.ErrUnauthorized},
		{"release from CHALLENGED is a status failure even when forced",
			types.InitiativeStatus_INITIATIVE_STATUS_CHALLENGED, true, true, types.ErrInvalidInitiativeStatus},
		{"release from COMPLETED is a status failure",
			types.InitiativeStatus_INITIATIVE_STATUS_COMPLETED, true, true, types.ErrInvalidInitiativeStatus},
		{"release from REJECTED is a status failure",
			types.InitiativeStatus_INITIATIVE_STATUS_REJECTED, true, true, types.ErrInvalidInitiativeStatus},
		{"close from CHALLENGED is a status failure",
			types.InitiativeStatus_INITIATIVE_STATUS_CHALLENGED, false, false, types.ErrInvalidInitiativeStatus},
		{"close from COMPLETED is a status failure",
			types.InitiativeStatus_INITIATIVE_STATUS_COMPLETED, false, false, types.ErrInvalidInitiativeStatus},
		{"close from CLOSED is a status failure",
			types.InitiativeStatus_INITIATIVE_STATUS_CLOSED, false, false, types.ErrInvalidInitiativeStatus},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rf := setupReview(t, 0)
			k, ctx := rf.f.keeper, rf.f.ctx

			initiative, err := k.GetInitiative(ctx, rf.initiative)
			require.NoError(t, err)
			initiative.Status = tc.status
			require.NoError(t, k.UpdateInitiative(ctx, initiative))

			if tc.release {
				err = k.UnassignInitiative(ctx, rf.initiative, "t", tc.forced)
			} else {
				err = k.CloseInitiative(ctx, rf.initiative, "t")
			}
			require.ErrorIs(t, err, tc.wantErr)
		})
	}
}

// An initiative with nobody holding it has nothing to release.
func TestReleaseWithNoAssigneeIsAStatusError(t *testing.T) {
	rf := setupReview(t, 0)
	k, ctx := rf.f.keeper, rf.f.ctx

	require.NoError(t, k.UnassignInitiative(ctx, rf.initiative, "first release", true))
	require.ErrorIs(t, k.UnassignInitiative(ctx, rf.initiative, "again", true),
		types.ErrInvalidInitiativeStatus)
}

// The msg servers wrap keeper errors with context. Registered codes must
// survive that wrapping, or the typing above buys nothing on the wire: the tx
// would still be delivered as code 1.
func TestGateErrorCodesSurviveMsgServerWrapping(t *testing.T) {
	rf := setupReview(t, 0)
	k, ctx := rf.f.keeper, rf.f.ctx
	ms := keeper.NewMsgServerImpl(k)

	initiative, err := k.GetInitiative(ctx, rf.initiative)
	require.NoError(t, err)
	initiative.Status = types.InitiativeStatus_INITIATIVE_STATUS_CHALLENGED
	require.NoError(t, k.UpdateInitiative(ctx, initiative))

	_, err = ms.UnassignInitiative(ctx, &types.MsgUnassignInitiative{
		Creator:      rf.assignee.String(),
		InitiativeId: rf.initiative,
		Reason:       "let me out",
	})
	require.ErrorIs(t, err, types.ErrInvalidInitiativeStatus)
	_, code, _ := errorsmod.ABCIInfo(err, false)
	require.Equal(t, types.ErrInvalidInitiativeStatus.ABCICode(), code,
		"the wrapped error must still deliver 1402, not the generic code 1")

	_, err = ms.CloseInitiative(ctx, &types.MsgCloseInitiative{
		Creator:      rf.creator.String(),
		InitiativeId: rf.initiative,
		Reason:       "retire it",
	})
	require.ErrorIs(t, err, types.ErrInvalidInitiativeStatus)
}

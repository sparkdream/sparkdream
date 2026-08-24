package keeper_test

import (
	"testing"

	repkeeper "sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

// These three queries cover state that was write-only: the verdicts that decide
// whether work completes, DREAM escrowed against an initiative, and the rounds
// waiting on a committee. Each has somebody who must act on it and had no way
// to read it.

func TestQueryInitiativeReviewsReportsTheGate(t *testing.T) {
	rf := setupReview(t, 1)
	k, ctx := rf.f.keeper, rf.f.ctx
	qs := repkeeper.NewQueryServerImpl(k)

	resp, err := qs.InitiativeReviews(ctx, &types.QueryInitiativeReviewsRequest{InitiativeId: rf.initiative})
	require.NoError(t, err)
	require.Len(t, resp.Rounds, 1, "round 0 is present even before any verdict")
	require.Empty(t, resp.Rounds[0].Reviews)
	require.Equal(t, uint32(0), resp.Approvals)
	require.Equal(t, uint32(1), resp.Required)
	require.False(t, resp.Satisfied, "an unreviewed round must not read as satisfied")

	require.NoError(t, k.SubmitInitiativeReview(ctx, rf.initiative, rf.reviewer.String(),
		true, nil, "looks done"))

	resp, err = qs.InitiativeReviews(ctx, &types.QueryInitiativeReviewsRequest{InitiativeId: rf.initiative})
	require.NoError(t, err)
	require.Len(t, resp.Rounds, 1)
	require.Len(t, resp.Rounds[0].Reviews, 1)
	require.Equal(t, uint32(1), resp.Approvals)
	require.True(t, resp.Satisfied)
	require.Equal(t, rf.reviewer.String(), resp.Rounds[0].Reviews[0].Reviewer)
}

func TestQueryInitiativeReviewsKeepsRejectedRoundsAddressable(t *testing.T) {
	// Round numbering starts at 0, so the codebase's "zero means unset" filter
	// convention would have made the first round unreachable. Returning every
	// round sidesteps that: a rejected round's verdicts stay auditable, which
	// is the whole reason someone looks this up after a bounce.
	rf := setupReview(t, 1)
	k, ctx := rf.f.keeper, rf.f.ctx
	qs := repkeeper.NewQueryServerImpl(k)

	require.NoError(t, k.SubmitInitiativeReview(ctx, rf.initiative, rf.reviewer.String(),
		false, nil, "not done"))

	resp, err := qs.InitiativeReviews(ctx, &types.QueryInitiativeReviewsRequest{InitiativeId: rf.initiative})
	require.NoError(t, err)
	require.Equal(t, uint32(1), resp.CurrentRound)
	require.Len(t, resp.Rounds, 2, "the closed round and the new one")

	require.Equal(t, uint32(0), resp.Rounds[0].Round)
	require.Len(t, resp.Rounds[0].Reviews, 1, "round 0's rejecting verdict is still readable")
	require.False(t, resp.Rounds[0].Reviews[0].Approved)
	require.Equal(t, uint32(0), resp.Rounds[0].Approvals)

	require.Equal(t, uint32(1), resp.Rounds[1].Round)
	require.Empty(t, resp.Rounds[1].Reviews, "the fresh round has no verdicts yet")
	require.Equal(t, uint32(0), resp.Approvals, "the summary tracks the CURRENT round")
	require.False(t, resp.Satisfied)
}

func TestQueryInitiativeReviewsUnknownInitiative(t *testing.T) {
	f := initFixture(t)
	qs := repkeeper.NewQueryServerImpl(f.keeper)
	_, err := qs.InitiativeReviews(f.ctx, &types.QueryInitiativeReviewsRequest{InitiativeId: 9999})
	require.Error(t, err)
	_, err = qs.InitiativeReviews(f.ctx, nil)
	require.Error(t, err)
}

func TestQueryReviewBountyReportsReclaimEligibility(t *testing.T) {
	rf := setupReview(t, 1)
	k, ctx := rf.f.keeper, rf.f.ctx
	qs := repkeeper.NewQueryServerImpl(k)

	funder := sdk.AccAddress([]byte("q-bounty-funder-"))
	mkReviewMember(t, k, ctx, funder, "100.0")
	_, err := k.EscrowReviewBounty(ctx, funder, rf.initiative, math.NewInt(1_000))
	require.NoError(t, err)

	params, err := k.Params.Get(ctx)
	require.NoError(t, err)

	// Before the delay: visible, but not yet reclaimable.
	resp, err := qs.ReviewBounty(ctx, &types.QueryReviewBountyRequest{InitiativeId: rf.initiative})
	require.NoError(t, err)
	require.Equal(t, math.NewInt(1_000), resp.Bounty.Amount)
	require.Len(t, resp.ReclaimStatus, 1)
	require.Equal(t, funder.String(), resp.ReclaimStatus[0].Funder)
	require.False(t, resp.ReclaimStatus[0].Reclaimable)
	require.Equal(t, ctx.BlockHeight()+int64(params.ReviewBountyReclaimDelay),
		resp.ReclaimStatus[0].ReclaimableAtHeight)

	// After the delay: reclaimable.
	matured := ctx.WithBlockHeight(resp.ReclaimStatus[0].ReclaimableAtHeight)
	resp, err = qs.ReviewBounty(matured, &types.QueryReviewBountyRequest{InitiativeId: rf.initiative})
	require.NoError(t, err)
	require.True(t, resp.ReclaimStatus[0].Reclaimable)

	// A filed verdict commits the escrow: reclaim goes false again even though
	// the clock has run. The query must agree with the handler, or a funder is
	// told they can withdraw something the chain will refuse.
	require.NoError(t, k.SubmitInitiativeReview(matured, rf.initiative, rf.reviewer.String(),
		true, nil, "done"))
	resp, err = qs.ReviewBounty(matured, &types.QueryReviewBountyRequest{InitiativeId: rf.initiative})
	require.NoError(t, err)
	require.True(t, resp.Bounty.Committed)
	require.False(t, resp.ReclaimStatus[0].Reclaimable,
		"committed beats the clock, exactly as WithdrawReviewBounty enforces")
}

func TestQueryReviewBountyEmptyIsNotAnError(t *testing.T) {
	f := initFixture(t)
	qs := repkeeper.NewQueryServerImpl(f.keeper)
	resp, err := qs.ReviewBounty(f.ctx, &types.QueryReviewBountyRequest{InitiativeId: 1})
	require.NoError(t, err, "an unfunded initiative reads as zero rather than not-found")
	require.True(t, resp.Bounty.Amount.IsZero())
	require.Empty(t, resp.ReclaimStatus)
}

func TestQueryEscalatedReviewsListsTheCommitteeQueue(t *testing.T) {
	rf := setupReview(t, 1)
	k := rf.f.keeper
	qs := repkeeper.NewQueryServerImpl(k)

	empty, err := qs.EscalatedReviews(rf.f.ctx, &types.QueryEscalatedReviewsRequest{})
	require.NoError(t, err)
	require.Empty(t, empty.Escalations)

	initiative, err := k.GetInitiative(rf.f.ctx, rf.initiative)
	require.NoError(t, err)
	ctx := rf.f.ctx.WithBlockHeight(initiative.ReviewDeadline + 1)
	require.NoError(t, k.SweepReviewDeadlines(ctx))

	listed, err := qs.EscalatedReviews(ctx, &types.QueryEscalatedReviewsRequest{})
	require.NoError(t, err)
	require.Len(t, listed.Escalations, 1,
		"escalation is not derivable from the initiative, so this is the committee's only view of its queue")
	require.Equal(t, rf.initiative, listed.Escalations[0].InitiativeId)
	require.NotZero(t, listed.Escalations[0].ReviewDeadline, "the committee needs its own deadline")
	require.NotEmpty(t, listed.Escalations[0].Title)
}

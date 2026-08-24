package keeper

import (
	"context"
	"fmt"

	"sparkdream/x/rep/types"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Read surfaces for the initiative-review machinery.
//
// All three cover state that was previously write-only: verdicts that decide
// whether work can complete, DREAM escrowed against an initiative, and the set
// of rounds waiting on a committee decision. Each is consulted by somebody who
// has to act on it, and none of them was reachable.

// InitiativeReviews returns every round's verdicts on one initiative, together
// with what the current round adds up to against the gate.
//
// No round selector: see QueryInitiativeReviewsRequest. max_review_rounds
// bounds the set at 3, so returning all of them is cheaper than the sentinel
// question it avoids.
func (q queryServer) InitiativeReviews(ctx context.Context, req *types.QueryInitiativeReviewsRequest) (*types.QueryInitiativeReviewsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	initiative, err := q.k.GetInitiative(ctx, req.InitiativeId)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "initiative %d not found", req.InitiativeId)
	}

	rounds := make([]types.InitiativeReviewRound, 0, initiative.ReviewRound+1)
	approvalsNow := uint32(0)
	for round := uint32(0); round <= initiative.ReviewRound; round++ {
		reviews, rErr := q.k.GetInitiativeReviews(ctx, req.InitiativeId, round)
		if rErr != nil {
			return nil, status.Error(codes.Internal, rErr.Error())
		}
		approvals := uint32(0)
		for _, r := range reviews {
			if r.Approved {
				approvals++
			}
		}
		if round == initiative.ReviewRound {
			approvalsNow = approvals
		}
		rounds = append(rounds, types.InitiativeReviewRound{
			Round:     round,
			Reviews:   reviews,
			Approvals: approvals,
		})
	}

	params, err := q.k.Params.Get(ctx)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	project, err := q.k.GetProject(ctx, initiative.ProjectId)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "project %d not found", initiative.ProjectId)
	}

	// `satisfied` is reported rather than left to the caller because
	// "approvals >= required" is not the whole rule — a committee escalation
	// can satisfy or fail the gate on its own.
	satisfied, err := q.k.ReviewGateSatisfied(ctx, params, initiative, project)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryInitiativeReviewsResponse{
		Rounds:       rounds,
		CurrentRound: initiative.ReviewRound,
		Approvals:    approvalsNow,
		Required:     RequiredVerifiersFor(params, initiative, project),
		Satisfied:    satisfied,
	}, nil
}

// ReviewBounty returns the escrow against an initiative and, per contribution,
// when it becomes reclaimable.
func (q queryServer) ReviewBounty(ctx context.Context, req *types.QueryReviewBountyRequest) (*types.QueryReviewBountyResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	bounty := q.k.GetReviewBounty(ctx, req.InitiativeId)

	params, err := q.k.Params.Get(ctx)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	height := sdk.UnwrapSDKContext(ctx).BlockHeight()

	statuses := make([]types.ReviewBountyReclaimStatus, 0, len(bounty.Contributions))
	for _, c := range bounty.Contributions {
		matureAt := c.FundedAt + int64(params.ReviewBountyReclaimDelay)
		amount := c.Amount
		if amount.IsNil() {
			amount = math.ZeroInt()
		}
		statuses = append(statuses, types.ReviewBountyReclaimStatus{
			Funder:              c.Funder,
			Amount:              amount,
			ReclaimableAtHeight: matureAt,
			// Committed wins over the clock: once a verdict is filed the escrow
			// belongs to paying for it, however long it has sat.
			Reclaimable: !bounty.Committed && height >= matureAt,
		})
	}

	return &types.QueryReviewBountyResponse{Bounty: bounty, ReclaimStatus: statuses}, nil
}

// EscalatedReviews lists the rounds awaiting an Operations Committee decision.
//
// Escalation lives in its own set because ReviewEscalation is reset to NONE
// when a round escalates, so it cannot be derived from the initiative. Without
// this the committee has no way to find the decisions waiting on it.
func (q queryServer) EscalatedReviews(ctx context.Context, req *types.QueryEscalatedReviewsRequest) (*types.QueryEscalatedReviewsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	out := []types.EscalatedReview{}
	if err := q.k.EscalatedReviews.Walk(ctx, nil, func(id uint64) (bool, error) {
		initiative, err := q.k.GetInitiative(ctx, id)
		if err != nil {
			// A marker whose initiative is gone is stale rather than fatal;
			// report the id so it is visible instead of silently dropped.
			out = append(out, types.EscalatedReview{InitiativeId: id, Title: "<initiative missing>"})
			return false, nil
		}
		out = append(out, types.EscalatedReview{
			InitiativeId:   id,
			Round:          initiative.ReviewRound,
			ReviewDeadline: initiative.ReviewDeadline,
			Title:          initiative.Title,
			Assignee:       initiative.Assignee,
		})
		return false, nil
	}); err != nil {
		return nil, status.Error(codes.Internal, fmt.Sprintf("walk escalated reviews: %v", err))
	}
	return &types.QueryEscalatedReviewsResponse{Escalations: out}, nil
}

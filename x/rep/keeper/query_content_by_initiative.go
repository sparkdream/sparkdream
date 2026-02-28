package keeper

import (
	"context"

	"sparkdream/x/rep/types"

	"cosmossdk.io/math"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) ContentByInitiative(ctx context.Context, req *types.QueryContentByInitiativeRequest) (*types.QueryContentByInitiativeResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	// Verify initiative exists
	if _, err := q.k.Initiative.Get(ctx, req.InitiativeId); err != nil {
		return nil, status.Error(codes.NotFound, "initiative not found")
	}

	// Get all linked content
	links, err := q.k.GetContentInitiativeLinks(ctx, req.InitiativeId)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	// Get propagation ratio
	params, err := q.k.Params.Get(ctx)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	entries := make([]types.ContentInitiativeLinkEntry, 0, len(links))
	totalConviction := math.LegacyZeroDec()

	for _, link := range links {
		conviction, err := q.k.GetContentConviction(ctx, types.StakeTargetType(link.TargetType), link.TargetID)
		if err != nil {
			conviction = math.LegacyZeroDec()
		}

		entries = append(entries, types.ContentInitiativeLinkEntry{
			TargetType: link.TargetType,
			TargetId:   link.TargetID,
			Conviction: conviction,
		})
		totalConviction = totalConviction.Add(conviction)
	}

	// Apply propagation ratio to get total propagated
	totalPropagated := totalConviction.Mul(params.ConvictionPropagationRatio)

	return &types.QueryContentByInitiativeResponse{
		Links:           entries,
		TotalPropagated: totalPropagated,
	}, nil
}

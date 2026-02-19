package keeper

import (
	"context"

	"sparkdream/x/collect/types"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) SponsorshipRequest(ctx context.Context, req *types.QuerySponsorshipRequestRequest) (*types.QuerySponsorshipRequestResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	sr, err := q.k.SponsorshipRequest.Get(ctx, req.CollectionId)
	if err != nil {
		return nil, status.Error(codes.NotFound, types.ErrSponsorshipRequestNotFound.Error())
	}

	return &types.QuerySponsorshipRequestResponse{SponsorshipRequest: sr}, nil
}

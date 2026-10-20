package keeper

import (
	"context"

	"cosmossdk.io/collections"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/artifact/types"
)

func (q queryServer) MediaProposals(ctx context.Context, req *types.QueryMediaProposalsRequest) (*types.QueryMediaProposalsResponse, error) {
	if req == nil {
		return nil, errInvalidRequest
	}
	if _, err := q.k.Tokens.Get(ctx, collections.Join(req.ClassId, req.TokenId)); err != nil {
		return nil, notFound(err, "token")
	}
	resp := &types.QueryMediaProposalsResponse{}
	err := q.k.MediaProposals.Walk(ctx, collections.NewSuperPrefixedTripleRange[uint64, uint64, uint64](req.ClassId, req.TokenId),
		func(_ collections.Triple[uint64, uint64, uint64], mp types.MediaProposal) (bool, error) {
			resp.Proposals = append(resp.Proposals, mp)
			return false, nil
		})
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return resp, nil
}

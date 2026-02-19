package keeper

import (
	"context"

	"cosmossdk.io/collections"

	"sparkdream/x/collect/types"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) Collaborators(ctx context.Context, req *types.QueryCollaboratorsRequest) (*types.QueryCollaboratorsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	var results []types.Collaborator

	// Walk CollaboratorReverse, filtering by collection_id (K2).
	// We walk all entries since there is no prefix index by collection_id on CollaboratorReverse.
	err := q.k.CollaboratorReverse.Walk(ctx, nil,
		func(key collections.Pair[string, uint64]) (bool, error) {
			if key.K2() != req.CollectionId {
				return false, nil
			}
			// Build composite key and get collaborator
			compositeKey := CollaboratorCompositeKey(req.CollectionId, key.K1())
			collab, err := q.k.Collaborator.Get(ctx, compositeKey)
			if err != nil {
				return false, nil
			}
			results = append(results, collab)
			return false, nil
		},
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryCollaboratorsResponse{Collaborators: results}, nil
}

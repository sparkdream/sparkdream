package keeper

import (
	"context"

	"sparkdream/x/service/types"

	"cosmossdk.io/collections"
	"github.com/cosmos/cosmos-sdk/types/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// CheckpointsByServiceType pages the latest checkpoint of every operator
// that has checkpointed under a service type. Records of operators that
// have since unbonded or been archived are kept as an audit trail; clients
// combine this with OperatorsByServiceType(ACTIVE) to decide whose
// checkpoints count.
func (q queryServer) CheckpointsByServiceType(ctx context.Context, req *types.QueryCheckpointsByServiceTypeRequest) (*types.QueryCheckpointsByServiceTypeResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	if req.ServiceType == "" {
		return nil, status.Error(codes.InvalidArgument, "service_type required")
	}

	checkpoints, pageRes, err := query.CollectionPaginate(
		ctx,
		q.k.Checkpoints,
		req.Pagination,
		func(_ collections.Pair[string, []byte], cp types.Checkpoint) (types.Checkpoint, error) {
			return cp, nil
		},
		query.WithCollectionPaginationPairPrefix[string, []byte](req.ServiceType),
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryCheckpointsByServiceTypeResponse{Checkpoints: checkpoints, Pagination: pageRes}, nil
}

package keeper

import (
	"context"

	"sparkdream/x/rep/types"

	"cosmossdk.io/collections"
	"github.com/cosmos/cosmos-sdk/types/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// AuthorBondsByType lists all author bond stakes for a bond target type,
// paginating over the (target_type, target_id, stake_id) index prefix.
func (q queryServer) AuthorBondsByType(ctx context.Context, req *types.QueryAuthorBondsByTypeRequest) (*types.QueryAuthorBondsByTypeResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	targetType := types.StakeTargetType(req.TargetType)
	if !types.IsAuthorBondType(targetType) {
		return nil, status.Errorf(codes.InvalidArgument, "target_type must be an author bond type (7, 8, 9, or 10), got %d", req.TargetType)
	}

	bonds, pageRes, err := query.CollectionFilteredPaginate(
		ctx,
		q.k.StakesByTarget,
		req.Pagination,
		func(key collections.Triple[int32, uint64, uint64], _ collections.NoValue) (bool, error) {
			// Skip stale index entries whose stake record no longer exists,
			// mirroring GetStakesByTarget.
			_, err := q.k.Stake.Get(ctx, key.K3())
			return err == nil, nil
		},
		func(key collections.Triple[int32, uint64, uint64], _ collections.NoValue) (types.Stake, error) {
			return q.k.Stake.Get(ctx, key.K3())
		},
		// The SDK has no WithCollectionPaginationTriplePrefix; set the
		// target_type prefix on the options directly.
		func(o *query.CollectionsPaginateOptions[collections.Triple[int32, uint64, uint64]]) {
			prefix := collections.TriplePrefix[int32, uint64, uint64](int32(targetType))
			o.Prefix = &prefix
		},
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryAuthorBondsByTypeResponse{
		Bonds:      bonds,
		Pagination: pageRes,
	}, nil
}

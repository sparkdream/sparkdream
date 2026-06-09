package keeper

import (
	"context"
	"errors"

	"sparkdream/x/forum/types"

	"cosmossdk.io/collections"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/types/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// GetPostConvictionStake returns a single post-conviction stake by id.
func (q queryServer) GetPostConvictionStake(ctx context.Context, req *types.QueryGetPostConvictionStakeRequest) (*types.QueryGetPostConvictionStakeResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	stake, err := q.k.PostConvictionStake.Get(ctx, req.Id)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, sdkerrors.ErrKeyNotFound
		}
		return nil, status.Error(codes.Internal, "internal error")
	}

	return &types.QueryGetPostConvictionStakeResponse{Stake: stake}, nil
}

// PostConvictionStakesByStaker lists a staker's open post-conviction stakes by
// prefix-walking the PostConvictionStakesByStaker index and resolving each id.
func (q queryServer) PostConvictionStakesByStaker(ctx context.Context, req *types.QueryPostConvictionStakesByStakerRequest) (*types.QueryPostConvictionStakesByStakerResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	if req.Staker == "" {
		return nil, status.Error(codes.InvalidArgument, "staker address required")
	}

	stakes, pageRes, err := query.CollectionPaginate(
		ctx,
		q.k.PostConvictionStakesByStaker,
		req.Pagination,
		func(key collections.Pair[string, uint64], _ collections.NoValue) (types.PostConvictionStake, error) {
			return q.k.PostConvictionStake.Get(ctx, key.K2())
		},
		query.WithCollectionPaginationPairPrefix[string, uint64](req.Staker),
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryPostConvictionStakesByStakerResponse{Stakes: stakes, Pagination: pageRes}, nil
}

// PostConvictionStakesByPost lists all active stakes backing a post by
// prefix-walking the PostConvictionStakesByPost index and resolving each id.
func (q queryServer) PostConvictionStakesByPost(ctx context.Context, req *types.QueryPostConvictionStakesByPostRequest) (*types.QueryPostConvictionStakesByPostResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	stakes, pageRes, err := query.CollectionPaginate(
		ctx,
		q.k.PostConvictionStakesByPost,
		req.Pagination,
		func(key collections.Pair[uint64, uint64], _ collections.NoValue) (types.PostConvictionStake, error) {
			return q.k.PostConvictionStake.Get(ctx, key.K2())
		},
		query.WithCollectionPaginationPairPrefix[uint64, uint64](req.PostId),
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryPostConvictionStakesByPostResponse{Stakes: stakes, Pagination: pageRes}, nil
}

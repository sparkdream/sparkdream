package keeper

import (
	"context"

	"github.com/cosmos/cosmos-sdk/types/query"

	"sparkdream/x/season/types"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) Titles(ctx context.Context, req *types.QueryTitlesRequest) (*types.QueryTitlesResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	// Use collection query for pagination
	titles, pageRes, err := query.CollectionPaginate(
		ctx,
		q.k.Title,
		req.Pagination,
		func(key string, title types.Title) (types.Title, error) {
			return title, nil
		},
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	if len(titles) == 0 {
		return &types.QueryTitlesResponse{
			Pagination: pageRes,
		}, nil
	}

	firstTitle := titles[0]
	return &types.QueryTitlesResponse{
		Id:         firstTitle.TitleId,
		Name:       firstTitle.Name,
		Rarity:     uint64(firstTitle.Rarity),
		Seasonal:   firstTitle.Seasonal,
		Pagination: pageRes,
	}, nil
}

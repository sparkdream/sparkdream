package keeper

import (
	"context"

	"sparkdream/x/forum/types"

	"github.com/cosmos/cosmos-sdk/types/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// FlagReviewQueue lists the posts whose flags put them in the sentinel review
// queue, paginated over the PostFlag store. The legacy single-post fields
// describe the first entry of the page.
func (q queryServer) FlagReviewQueue(ctx context.Context, req *types.QueryFlagReviewQueueRequest) (*types.QueryFlagReviewQueueResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	flags, pageRes, err := query.CollectionFilteredPaginate(
		ctx,
		q.k.PostFlag,
		req.Pagination,
		func(_ uint64, flag types.PostFlag) (bool, error) {
			return flag.InReviewQueue, nil
		},
		func(_ uint64, flag types.PostFlag) (types.PostFlag, error) {
			return flag, nil
		},
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	resp := &types.QueryFlagReviewQueueResponse{Flags: flags, Pagination: pageRes}
	if len(flags) > 0 {
		resp.PostId = flags[0].PostId
		resp.TotalWeight = flags[0].TotalWeight
	}
	return resp, nil
}

package keeper

import (
	"context"

	"sparkdream/x/season/types"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ListNominationsByCreator returns nominations filtered by creator address.
func (q queryServer) ListNominationsByCreator(ctx context.Context, req *types.QueryListNominationsByCreatorRequest) (*types.QueryListNominationsByCreatorResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	if req.Creator == "" {
		return nil, status.Error(codes.InvalidArgument, "creator address required")
	}

	iter, err := q.k.Nomination.Iterate(ctx, nil)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	defer iter.Close()

	var nominations []types.Nomination
	for ; iter.Valid(); iter.Next() {
		nomination, err := iter.Value()
		if err != nil {
			continue
		}
		if nomination.Nominator == req.Creator {
			nominations = append(nominations, nomination)
		}
	}

	return &types.QueryListNominationsByCreatorResponse{
		Nominations: nominations,
	}, nil
}

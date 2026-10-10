package keeper

import (
	"context"

	"sparkdream/x/artifact/types"
)

func (q queryServer) HideRecord(ctx context.Context, req *types.QueryHideRecordRequest) (*types.QueryHideRecordResponse, error) {
	if req == nil {
		return nil, errInvalidRequest
	}
	hr, err := q.k.HideRecords.Get(ctx, req.HideId)
	if err != nil {
		return nil, notFound(err, "hide record")
	}
	return &types.QueryHideRecordResponse{Record: hr}, nil
}

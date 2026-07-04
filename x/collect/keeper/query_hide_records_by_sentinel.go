package keeper

import (
	"context"

	"github.com/cosmos/cosmos-sdk/types/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/collect/types"
)

// HideRecordsBySentinel returns the hide records created by a sentinel, most
// recent first. There is deliberately no by-sentinel state index: hide records
// are rare moderation events, so a filtered walk over the HideRecord map keeps
// the query consensus-neutral (no new writes, no store migration for existing
// records). Council (gov) hides carry no sentinel address and never match.
func (q queryServer) HideRecordsBySentinel(ctx context.Context, req *types.QueryHideRecordsBySentinelRequest) (*types.QueryHideRecordsBySentinelResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	if _, err := q.k.addressCodec.StringToBytes(req.Sentinel); err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid sentinel address")
	}

	var matched []types.HideRecord
	err := q.k.HideRecord.Walk(ctx, nil, func(_ uint64, hr types.HideRecord) (bool, error) {
		if hr.Sentinel == req.Sentinel {
			matched = append(matched, hr)
		}
		return false, nil
	})
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	// Most recent hide first (ids are sequential, walk yields ascending).
	for i, j := 0, len(matched)-1; i < j; i, j = i+1, j-1 {
		matched[i], matched[j] = matched[j], matched[i]
	}

	pageReq := req.Pagination
	var offset, limit uint64 = 0, 100
	if pageReq != nil {
		offset = pageReq.Offset
		if pageReq.Limit > 0 && pageReq.Limit <= 100 {
			limit = pageReq.Limit
		}
	}
	total := uint64(len(matched))
	results := paginate(matched, offset, limit)

	return &types.QueryHideRecordsBySentinelResponse{
		HideRecords: results,
		Pagination:  &query.PageResponse{Total: total},
	}, nil
}

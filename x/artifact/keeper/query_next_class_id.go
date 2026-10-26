package keeper

import (
	"context"

	"sparkdream/x/artifact/types"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// NextClassId returns the id the next MsgCreateClass assigns (ids start at 1).
func (q queryServer) NextClassId(ctx context.Context, req *types.QueryNextClassIdRequest) (*types.QueryNextClassIdResponse, error) {
	if req == nil {
		return nil, errInvalidRequest
	}
	seq, err := q.k.ClassSeq.Peek(ctx)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &types.QueryNextClassIdResponse{NextClassId: seq + 1}, nil
}

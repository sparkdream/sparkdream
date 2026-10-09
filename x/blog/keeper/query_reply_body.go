package keeper

import (
	"context"

	"sparkdream/x/blog/types"

	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ReplyBody returns a reply's stored body, including bodies ShowReply and
// ListReplies withhold because media_flags != 0.
func (q queryServer) ReplyBody(ctx context.Context, req *types.QueryReplyBodyRequest) (*types.QueryReplyBodyResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	reply, found := q.k.GetReply(ctx, req.Id)
	if !found {
		return nil, sdkerrors.ErrKeyNotFound
	}

	return &types.QueryReplyBodyResponse{
		Body:        reply.Body,
		ContentType: reply.ContentType,
		MediaFlags:  reply.MediaFlags,
		BodyHash:    reply.BodyHash,
	}, nil
}

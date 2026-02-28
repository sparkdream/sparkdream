package keeper

import (
	"context"
	"encoding/binary"

	"sparkdream/x/blog/types"

	"cosmossdk.io/store/prefix"
	"github.com/cosmos/cosmos-sdk/runtime"
	"github.com/cosmos/cosmos-sdk/types/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) ListReplies(ctx context.Context, req *types.QueryListRepliesRequest) (*types.QueryListRepliesResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	storeAdapter := runtime.KVStoreAdapter(q.k.storeService.OpenKVStore(ctx))
	postIndexStore := prefix.NewStore(storeAdapter, []byte(types.ReplyPostKey))
	postPrefix := GetPostIDBytes(req.PostId)
	postReplyStore := prefix.NewStore(postIndexStore, postPrefix)

	var replies []types.Reply
	pageRes, err := query.Paginate(postReplyStore, req.Pagination, func(key []byte, value []byte) error {
		if len(key) < 8 {
			return nil
		}
		replyId := binary.BigEndian.Uint64(key[:8])
		reply, found := q.k.GetReply(ctx, replyId)
		if !found {
			return nil
		}
		if reply.Status == types.ReplyStatus_REPLY_STATUS_DELETED {
			return nil
		}
		if reply.Status == types.ReplyStatus_REPLY_STATUS_HIDDEN && !req.IncludeHidden {
			return nil
		}
		if req.FilterByParent && reply.ParentReplyId != req.ParentReplyId {
			return nil
		}
		replies = append(replies, reply)
		return nil
	})
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryListRepliesResponse{Replies: replies, Pagination: pageRes}, nil
}

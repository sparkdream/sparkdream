package keeper

import (
	"context"
	"encoding/binary"
	"strings"

	"sparkdream/x/blog/types"

	"cosmossdk.io/store/prefix"
	"github.com/cosmos/cosmos-sdk/runtime"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) ListExpiringContent(ctx context.Context, req *types.QueryListExpiringContentRequest) (*types.QueryListExpiringContentResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	storeAdapter := runtime.KVStoreAdapter(q.k.storeService.OpenKVStore(ctx))
	store := prefix.NewStore(storeAdapter, []byte(types.ExpiryKey))

	var posts []types.Post
	var replies []types.Reply

	iterator := store.Iterator(nil, nil)
	defer iterator.Close()

	for ; iterator.Valid(); iterator.Next() {
		key := iterator.Key()
		if len(key) < 8 {
			continue
		}
		ts := binary.BigEndian.Uint64(key[:8])
		if req.ExpiresBefore > 0 && int64(ts) > req.ExpiresBefore {
			break
		}

		// Key format after timestamp: "/{type}/{id_8bytes}"
		remaining := string(key[8:])
		var contentType string
		var idBytes []byte
		if strings.HasPrefix(remaining, "/post/") {
			contentType = "post"
			idBytes = key[8+6:]
		} else if strings.HasPrefix(remaining, "/reply/") {
			contentType = "reply"
			idBytes = key[8+7:]
		} else {
			continue
		}

		if req.ContentType != "" && req.ContentType != contentType {
			continue
		}

		if len(idBytes) < 8 {
			continue
		}
		id := binary.BigEndian.Uint64(idBytes[:8])

		if contentType == "post" {
			post, found := q.k.GetPost(ctx, id)
			if found && post.Status != types.PostStatus_POST_STATUS_DELETED {
				posts = append(posts, post)
			}
		} else {
			reply, found := q.k.GetReply(ctx, id)
			if found && reply.Status != types.ReplyStatus_REPLY_STATUS_DELETED {
				replies = append(replies, reply)
			}
		}
	}

	return &types.QueryListExpiringContentResponse{Posts: posts, Replies: replies}, nil
}

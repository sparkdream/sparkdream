package keeper

import (
	"context"
	"encoding/binary"
	commontypes "sparkdream/x/common/types"
	"strings"

	"sparkdream/x/blog/types"

	"cosmossdk.io/store/prefix"
	"github.com/cosmos/cosmos-sdk/runtime"
	"github.com/cosmos/cosmos-sdk/types/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) ListExpiringContent(ctx context.Context, req *types.QueryListExpiringContentRequest) (*types.QueryListExpiringContentResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	storeAdapter := runtime.KVStoreAdapter(q.k.storeService.OpenKVStore(ctx))
	store := prefix.NewStore(storeAdapter, []byte(types.ExpiryKey))

	// Key-based pagination over the expiry index (ordered by expiry time).
	// limit caps the results; every visited index key, including ones the
	// content-type filter or a tombstone skips, counts against a scan budget,
	// so one query never walks the whole index. When the query stops early
	// it returns next_key to resume from. Offsets are not supported.
	limit := uint64(maxExpiringResults)
	var startKey []byte
	if req.Pagination != nil {
		if req.Pagination.Limit > 0 && req.Pagination.Limit < limit {
			limit = req.Pagination.Limit
		}
		startKey = req.Pagination.Key
	}
	scanBudget := limit * expiringScanFactor

	var posts []types.Post
	var replies []types.Reply
	resultCount := uint64(0)
	scanned := uint64(0)
	var nextKey []byte

	iterator := store.Iterator(startKey, nil)
	defer iterator.Close()

	for ; iterator.Valid(); iterator.Next() {
		if resultCount >= limit || scanned >= scanBudget {
			nextKey = append([]byte{}, iterator.Key()...)
			break
		}
		scanned++
		key := iterator.Key()
		if len(key) < 8 {
			continue
		}
		ts := binary.BigEndian.Uint64(key[:8])
		if req.ExpiresBefore > 0 && commontypes.Saturate[int64](ts) > req.ExpiresBefore {
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
				posts = append(posts, withholdPostBody(post))
				resultCount++
			}
		} else {
			reply, found := q.k.GetReply(ctx, id)
			if found && reply.Status != types.ReplyStatus_REPLY_STATUS_DELETED {
				replies = append(replies, withholdReplyBody(reply))
				resultCount++
			}
		}
	}

	return &types.QueryListExpiringContentResponse{
		Posts:      posts,
		Replies:    replies,
		Pagination: &query.PageResponse{NextKey: nextKey},
	}, nil
}

const (
	// maxExpiringResults caps one page of ListExpiringContent.
	maxExpiringResults = 100
	// expiringScanFactor bounds the index keys one page may visit: limit x
	// this factor.
	expiringScanFactor = 10
)

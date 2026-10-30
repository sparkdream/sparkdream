package keeper

import (
	"bytes"
	"context"
	"encoding/binary"

	"sparkdream/x/blog/types"

	"cosmossdk.io/store/prefix"
	"github.com/cosmos/cosmos-sdk/runtime"
	"github.com/cosmos/cosmos-sdk/types/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (q queryServer) ListPostsByCreator(ctx context.Context, req *types.QueryListPostsByCreatorRequest) (*types.QueryListPostsByCreatorResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	storeAdapter := runtime.KVStoreAdapter(q.k.storeService.OpenKVStore(ctx))
	creatorStore := prefix.NewStore(storeAdapter, []byte(types.CreatorPostKey))

	// Tombstoned posts are listed (status DELETED, empty title/body); hidden
	// ones only with include_hidden.
	collect := func(posts []types.Post, idKey []byte) []types.Post {
		if len(idKey) < 8 {
			return posts
		}
		postId := binary.BigEndian.Uint64(idKey[:8])
		post, found := q.k.GetPost(ctx, postId)
		if !found {
			return posts
		}
		if post.Status == types.PostStatus_POST_STATUS_HIDDEN && !req.IncludeHidden {
			return posts
		}
		return append(posts, withholdPostBody(post))
	}

	// Alias contract: posts written from the member's past addresses
	// (pre-migration) sit under their own index prefixes. The canonical and
	// alias prefixes are paginated as one ordered stream (canonical first),
	// so limit bounds every page and no page repeats another's posts.
	// next_key is the full creator-index key ("{address}/{id}") to resume at.
	aliases := q.k.allAddressesOf(ctx, req.Creator)

	var (
		offset, limit uint64
		startKey      []byte
		countTotal    bool
	)
	if req.Pagination != nil {
		offset, limit, startKey, countTotal = req.Pagination.Offset, req.Pagination.Limit, req.Pagination.Key, req.Pagination.CountTotal
	}
	if limit == 0 {
		limit = query.DefaultLimit
	}
	if len(startKey) > 0 {
		offset = 0
	}

	var (
		posts   []types.Post
		nextKey []byte
		seen    uint64 // index keys passed, for offset and total
		taken   uint64
	)
	started := len(startKey) == 0
	for _, alias := range aliases {
		pfx := []byte(alias + "/")
		var from []byte
		if !started {
			if !bytes.HasPrefix(startKey, pfx) {
				continue
			}
			from = startKey[len(pfx):]
			started = true
		}
		it := prefix.NewStore(creatorStore, pfx).Iterator(from, nil)
		for ; it.Valid(); it.Next() {
			if taken >= limit {
				if nextKey == nil {
					nextKey = append(append([]byte{}, pfx...), it.Key()...)
				}
				if !countTotal {
					break
				}
				seen++
				continue
			}
			seen++
			if seen <= offset {
				continue
			}
			posts = collect(posts, it.Key())
			taken++
		}
		it.Close()
		if nextKey != nil && !countTotal {
			break
		}
	}

	pageRes := &query.PageResponse{NextKey: nextKey}
	if countTotal {
		pageRes.Total = seen
	}
	return &types.QueryListPostsByCreatorResponse{Posts: posts, Pagination: pageRes}, nil
}

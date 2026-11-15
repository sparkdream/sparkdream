package keeper

import (
	"context"
	"encoding/binary"

	"github.com/cosmos/cosmos-sdk/types/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/blog/types"
)

// ListPinnedPosts returns paginated pinned posts, most recently pinned first,
// from the pinned post index. Hidden and deleted posts keep their index entry
// (they are still pinned, and reappear if unhidden) but are skipped here, so
// a page may hold fewer posts than its limit; use next_key to detect the end.
func (q queryServer) ListPinnedPosts(ctx context.Context, req *types.QueryListPinnedPostsRequest) (*types.QueryListPinnedPostsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	var posts []types.Post
	pageRes, err := query.Paginate(q.k.pinnedPostStore(ctx), req.Pagination, func(key []byte, _ []byte) error {
		if len(key) < 16 {
			return nil
		}
		post, found := q.k.GetPost(ctx, binary.BigEndian.Uint64(key[8:16]))
		if !found || post.PinnedBy == "" {
			return nil
		}
		if post.Status == types.PostStatus_POST_STATUS_DELETED || post.Status == types.PostStatus_POST_STATUS_HIDDEN {
			return nil
		}
		posts = append(posts, withholdPostBody(post))
		return nil
	})
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	return &types.QueryListPinnedPostsResponse{Posts: posts, Pagination: pageRes}, nil
}

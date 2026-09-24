package keeper

import (
	"context"
	"strings"

	"cosmossdk.io/collections"

	"sparkdream/x/federation/types"

	query "github.com/cosmos/cosmos-sdk/types/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ListFederatedContent pages federated content, optionally filtered by
// peer, content type, creator identity, and status name. When an index
// exists for one of the filters (peer, creator, or type) the walk runs
// over that index instead of the whole Content map; remaining filters
// apply inline. Offset/limit pagination over the chosen range, following
// the query_curation_reviews_by_curator precedent — the primary consumer
// (the verifier runner) polls with a fresh offset each cycle, so key
// pagination across a mutating filtered set is not worth its complexity.
func (q queryServer) ListFederatedContent(ctx context.Context, req *types.QueryListFederatedContentRequest) (*types.QueryListFederatedContentResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	var statusFilter types.FederatedContentStatus
	if req.Status != "" {
		val, ok := types.FederatedContentStatus_value[strings.TrimSpace(req.Status)]
		if !ok {
			return nil, status.Errorf(codes.InvalidArgument,
				"unknown status %q (use the full enum name, e.g. FEDERATED_CONTENT_STATUS_PENDING_VERIFICATION)", req.Status)
		}
		statusFilter = types.FederatedContentStatus(val)
	}

	matches := func(c types.FederatedContent) bool {
		return (req.PeerId == "" || c.PeerId == req.PeerId) &&
			(req.ContentType == "" || c.ContentType == req.ContentType) &&
			(req.CreatorIdentity == "" || c.CreatorIdentity == req.CreatorIdentity) &&
			(req.Status == "" || c.Status == statusFilter)
	}

	// No filters at all: keep the original whole-collection paginate.
	if req.PeerId == "" && req.ContentType == "" && req.CreatorIdentity == "" && req.Status == "" {
		content, pageRes, err := query.CollectionPaginate(ctx, q.k.Content, req.Pagination, func(key uint64, value types.FederatedContent) (types.FederatedContent, error) {
			return value, nil
		})
		if err != nil {
			return nil, status.Error(codes.Internal, err.Error())
		}
		return &types.QueryListFederatedContentResponse{Content: content, Pagination: pageRes}, nil
	}

	pageReq := req.Pagination
	if pageReq == nil {
		pageReq = &query.PageRequest{Limit: 100}
	}
	limit := pageReq.Limit
	if limit == 0 {
		limit = 100
	}
	offset := pageReq.Offset

	// offset, matched and the returned Total are all counted in MATCH
	// space, not in candidates-walked: a caller can only ever observe
	// matches, so an offset denominated in scanned index entries is
	// unusable — advancing it by len(results) would re-read rows it has
	// already seen. `matched` keeps counting past the page so Total is a
	// real total; only `results` stops growing at limit.
	var matched uint64
	var results []types.FederatedContent

	consider := func(content types.FederatedContent) {
		if !matches(content) {
			return
		}
		matched++
		if matched <= offset {
			return
		}
		if uint64(len(results)) >= limit {
			return
		}
		results = append(results, content)
	}

	appendIfMatch := func(contentID uint64) (bool, error) {
		content, err := q.k.Content.Get(ctx, contentID)
		if err != nil {
			// Index entry with no primary record: skip it rather than
			// failing the whole page. Not counted as a match.
			return false, nil
		}
		consider(content)
		return false, nil
	}

	var err error
	switch {
	case req.PeerId != "":
		err = q.k.ContentByPeer.Walk(ctx,
			collections.NewPrefixedPairRange[string, uint64](req.PeerId),
			func(key collections.Pair[string, uint64]) (bool, error) {
				return appendIfMatch(key.K2())
			})
	case req.CreatorIdentity != "":
		err = q.k.ContentByCreator.Walk(ctx,
			collections.NewPrefixedPairRange[string, uint64](req.CreatorIdentity),
			func(key collections.Pair[string, uint64]) (bool, error) {
				return appendIfMatch(key.K2())
			})
	case req.ContentType != "":
		err = q.k.ContentByType.Walk(ctx,
			collections.NewPrefixedPairRange[string, uint64](req.ContentType),
			func(key collections.Pair[string, uint64]) (bool, error) {
				return appendIfMatch(key.K2())
			})
	default:
		// Only a status filter — status has no index; scan the primary
		// collection.
		err = q.k.Content.Walk(ctx, nil, func(_ uint64, value types.FederatedContent) (bool, error) {
			consider(value)
			return false, nil
		})
	}
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	// No NextKey: this branch is offset-paginated, and Total is the real
	// match count, so a caller pages with offset += limit until
	// offset >= Total.
	return &types.QueryListFederatedContentResponse{
		Content:    results,
		Pagination: &query.PageResponse{Total: matched},
	}, nil
}

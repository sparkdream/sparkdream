package keeper

import (
	"context"
	"errors"

	"cosmossdk.io/collections"
	"github.com/cosmos/cosmos-sdk/types/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/storage/types"
)

// view prepares a request for a query response. While its hash is flagged
// the source addresses and locators are returned empty and withheld is true:
// the chain should not go on pointing at a file its scanners removed. This
// is a default for clients, not secrecy; RequestLocation returns them
// regardless.
func (q queryServer) view(ctx context.Context, r types.Request) (out types.Request, withheld bool, err error) {
	flagged, err := q.k.RemovedHashes.Has(ctx, r.Sha256)
	if err != nil {
		return types.Request{}, false, err
	}
	if !flagged {
		return r, false, nil
	}
	r.Locator = ""
	r.VerdictsLocator = ""
	sources := make([]types.Source, len(r.Sources))
	for i, s := range r.Sources {
		s.Url = ""
		sources[i] = s
	}
	r.Sources = sources
	return r, true, nil
}

func (q queryServer) Backend(ctx context.Context, req *types.QueryBackendRequest) (*types.QueryBackendResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	b, err := q.k.Backends.Get(ctx, req.Name)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "backend not found")
		}
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &types.QueryBackendResponse{Backend: b}, nil
}

func (q queryServer) Backends(ctx context.Context, req *types.QueryBackendsRequest) (*types.QueryBackendsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	backends, page, err := query.CollectionPaginate(ctx, q.k.Backends, req.Pagination,
		func(_ string, b types.Backend) (types.Backend, error) { return b, nil })
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &types.QueryBackendsResponse{Backends: backends, Pagination: page}, nil
}

func (q queryServer) ApprovedOperators(ctx context.Context, req *types.QueryApprovedOperatorsRequest) (*types.QueryApprovedOperatorsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	b, err := q.k.Backends.Get(ctx, req.Backend)
	if err != nil {
		return nil, status.Error(codes.NotFound, "backend not found")
	}
	ops, page, err := query.CollectionPaginate(ctx, q.k.ApprovedOperators, req.Pagination,
		func(key collections.Pair[string, string], a types.Approval) (types.ApprovedOperator, error) {
			stale := true
			if q.k.late.service != nil {
				_, counts := q.k.operatorApproval(ctx, b, key.K2())
				stale = !counts
			}
			return types.ApprovedOperator{Backend: key.K1(), Operator: key.K2(), Controller: a.Controller, Height: a.Height, Stale: stale}, nil
		},
		query.WithCollectionPaginationPairPrefix[string, string](req.Backend))
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &types.QueryApprovedOperatorsResponse{Operators: ops, Pagination: page}, nil
}

func (q queryServer) ApprovedScanners(ctx context.Context, req *types.QueryApprovedScannersRequest) (*types.QueryApprovedScannersResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	scanners, page, err := query.CollectionPaginate(ctx, q.k.ApprovedScanners, req.Pagination,
		func(scanner string, a types.Approval) (types.ApprovedScanner, error) {
			stale := true
			if q.k.late.service != nil {
				op, live := q.k.liveOperator(ctx, scanner, types.ContentScannerServiceType)
				stale = !live || op.Controller != a.Controller
			}
			return types.ApprovedScanner{Scanner: scanner, Controller: a.Controller, Height: a.Height, Stale: stale}, nil
		})
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &types.QueryApprovedScannersResponse{Scanners: scanners, Pagination: page}, nil
}

func (q queryServer) OperatorStanding(ctx context.Context, req *types.QueryOperatorStandingRequest) (*types.QueryOperatorStandingResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	if err := q.k.wired(); err != nil {
		return nil, status.Error(codes.Unavailable, err.Error())
	}
	b, err := q.k.Backends.Get(ctx, req.Backend)
	if err != nil {
		return nil, status.Error(codes.NotFound, "backend not found")
	}
	p, err := q.k.Params.Get(ctx)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	now := sdkHeight(ctx)
	res := &types.QueryOperatorStandingResponse{}
	_, res.Approved = q.k.operatorApproval(ctx, b, req.Operator)
	for kind, dst := range map[types.CounterKind]*types.Counter{
		types.CounterKind_COUNTER_KIND_LAPSE:               &res.Lapses,
		types.CounterKind_COUNTER_KIND_DECLINE_MISMATCH:    &res.MismatchDeclines,
		types.CounterKind_COUNTER_KIND_DECLINE_UNREACHABLE: &res.UnreachableDeclines,
		types.CounterKind_COUNTER_KIND_COMPLETION:          &res.Completions,
	} {
		c, err := q.k.counter(ctx, req.Operator, kind, now, p)
		if err != nil {
			return nil, status.Error(codes.Internal, err.Error())
		}
		*dst = c
	}
	res.OpenClaims, err = q.k.countClaims(ctx, req.Operator, p.MaxOpenClaimsPerOperator)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	switch _, cerr := q.k.couldClaim(ctx, b, req.Operator); {
	case cerr != nil:
		res.Reason = cerr.Error()
	case res.OpenClaims >= p.MaxOpenClaimsPerOperator:
		res.Reason = types.ErrTooManyClaims.Error()
	case res.Lapses.Count > uint64(p.MaxLapsesPerWindow):
		res.Reason = types.ErrLapseLimit.Error()
	default:
		res.CanClaim = true
	}
	return res, nil
}

func (q queryServer) Request(ctx context.Context, req *types.QueryRequestRequest) (*types.QueryRequestResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	r, err := q.k.Requests.Get(ctx, req.Id)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "request not found")
		}
		return nil, status.Error(codes.Internal, err.Error())
	}
	v, withheld, err := q.view(ctx, r)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &types.QueryRequestResponse{Request: v, Withheld: withheld}, nil
}

func (q queryServer) RequestByHash(ctx context.Context, req *types.QueryRequestByHashRequest) (*types.QueryRequestByHashResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	id, err := q.k.LiveByHash.Get(ctx, collections.Join(req.Backend, req.Sha256))
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "no live or settled request for that file")
		}
		return nil, status.Error(codes.Internal, err.Error())
	}
	r, err := q.k.Requests.Get(ctx, id)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	v, withheld, err := q.view(ctx, r)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &types.QueryRequestByHashResponse{Request: v, Withheld: withheld}, nil
}

// load fetches and prepares one request named by an index key, and notes its
// id in withheld when its addresses and locators were blanked.
func (q queryServer) load(ctx context.Context, id uint64, withheld *[]uint64) (types.Request, error) {
	r, err := q.k.Requests.Get(ctx, id)
	if err != nil {
		return types.Request{}, err
	}
	v, blanked, err := q.view(ctx, r)
	if err != nil {
		return types.Request{}, err
	}
	if blanked {
		*withheld = append(*withheld, id)
	}
	return v, nil
}

func (q queryServer) RequestsByStatus(ctx context.Context, req *types.QueryRequestsByStatusRequest) (*types.QueryRequestsByStatusResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	var withheld []uint64
	requests, page, err := query.CollectionPaginate(ctx, q.k.ByStatus, req.Pagination,
		func(key collections.Pair[string, uint64], _ collections.NoValue) (types.Request, error) {
			return q.load(ctx, key.K2(), &withheld)
		},
		query.WithCollectionPaginationPairPrefix[string, uint64](statusKey(req.Backend, req.Status)))
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &types.QueryRequestsByStatusResponse{Requests: requests, Pagination: page, WithheldIds: withheld}, nil
}

func (q queryServer) RequestsByRequester(ctx context.Context, req *types.QueryRequestsByRequesterRequest) (*types.QueryRequestsByRequesterResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	var withheld []uint64
	// A migrated member's requests stay under the address that made them.
	// The usual case is one address, paginated in the store; a member with
	// several is listed address by address in one page.
	addrs := []string{req.Requester}
	if q.k.late.rep != nil {
		addrs = q.k.late.rep.AllAddressesOf(ctx, req.Requester)
	}
	if len(addrs) == 1 {
		requests, page, err := query.CollectionPaginate(ctx, q.k.ByRequester, req.Pagination,
			func(key collections.Pair[string, uint64], _ collections.NoValue) (types.Request, error) {
				return q.load(ctx, key.K2(), &withheld)
			},
			query.WithCollectionPaginationPairPrefix[string, uint64](addrs[0]))
		if err != nil {
			return nil, status.Error(codes.Internal, err.Error())
		}
		return &types.QueryRequestsByRequesterResponse{Requests: requests, Pagination: page, WithheldIds: withheld}, nil
	}
	limit := uint64(query.DefaultLimit)
	if req.Pagination != nil && req.Pagination.Limit > 0 {
		limit = req.Pagination.Limit
	}
	var offset uint64
	if req.Pagination != nil {
		offset = req.Pagination.Offset
	}
	var requests []types.Request
	var seen uint64
	for _, a := range addrs {
		err := q.k.ByRequester.Walk(ctx, collections.NewPrefixedPairRange[string, uint64](a), func(key collections.Pair[string, uint64]) (bool, error) {
			seen++
			if seen <= offset || uint64(len(requests)) >= limit {
				return false, nil
			}
			r, err := q.load(ctx, key.K2(), &withheld)
			if err != nil {
				return true, err
			}
			requests = append(requests, r)
			return false, nil
		})
		if err != nil {
			return nil, status.Error(codes.Internal, err.Error())
		}
	}
	return &types.QueryRequestsByRequesterResponse{Requests: requests, Pagination: &query.PageResponse{Total: seen}, WithheldIds: withheld}, nil
}

func (q queryServer) RequestsByOperator(ctx context.Context, req *types.QueryRequestsByOperatorRequest) (*types.QueryRequestsByOperatorResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	var withheld []uint64
	requests, page, err := query.CollectionPaginate(ctx, q.k.ByOperator, req.Pagination,
		func(key collections.Pair[string, uint64], _ collections.NoValue) (types.Request, error) {
			return q.load(ctx, key.K2(), &withheld)
		},
		query.WithCollectionPaginationPairPrefix[string, uint64](req.Operator))
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &types.QueryRequestsByOperatorResponse{Requests: requests, Pagination: page, WithheldIds: withheld}, nil
}

func (q queryServer) RemovedHash(ctx context.Context, req *types.QueryRemovedHashRequest) (*types.QueryRemovedHashResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	rec, err := q.k.RemovedHashes.Get(ctx, req.Sha256)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "hash is not flagged")
		}
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &types.QueryRemovedHashResponse{RemovedHash: rec}, nil
}

func (q queryServer) QuoteFee(ctx context.Context, req *types.QueryQuoteFeeRequest) (*types.QueryQuoteFeeResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	b, err := q.k.Backends.Get(ctx, req.Backend)
	if err != nil {
		return nil, status.Error(codes.NotFound, "backend not found")
	}
	t, ok := b.FindType(req.ContentType)
	if !ok {
		return nil, status.Error(codes.InvalidArgument, types.ErrContentTypeNotAllowed.Error())
	}
	if req.SizeBytes == 0 || req.SizeBytes > t.MaxSize {
		return nil, status.Error(codes.InvalidArgument, types.ErrFileTooLarge.Error())
	}
	p, err := q.k.Params.Get(ctx)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	fee := b.CopyFee(req.SizeBytes)
	return &types.QueryQuoteFeeResponse{CopyFee: fee, RequestFee: p.RequestFee, Total: fee.Add(p.RequestFee)}, nil
}

func (q queryServer) RequestLocation(ctx context.Context, req *types.QueryRequestLocationRequest) (*types.QueryRequestLocationResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	r, err := q.k.Requests.Get(ctx, req.Id)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "request not found")
		}
		return nil, status.Error(codes.Internal, err.Error())
	}
	flagged, err := q.k.RemovedHashes.Has(ctx, r.Sha256)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &types.QueryRequestLocationResponse{Sources: r.Sources, Locator: r.Locator, VerdictsLocator: r.VerdictsLocator, Flagged: flagged}, nil
}

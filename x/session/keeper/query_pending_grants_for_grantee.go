package keeper

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/session/types"
)

// PendingGrantsForGrantee pages through the PENDING access-class grants
// (session keys, sign-ins) that name a grantee and await its acceptance
// (SES-202). They are on no other grantee-keyed index, so this is the
// grantee's discovery path to the grant id MsgAcceptGrant and
// MsgDeclineGrant take. Strangers can create such grants, so a client
// follows next_key rather than treating one page as complete; the page is
// capped at maxQueryResults like every other grant listing.
func (q queryServer) PendingGrantsForGrantee(ctx context.Context, req *types.QueryPendingGrantsForGranteeRequest) (*types.QueryPendingGrantsForGranteeResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	if _, err := q.k.addressCodec.StringToBytes(req.Grantee); err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid grantee address")
	}
	grants, page, err := paginateGrants(ctx, q.k, q.k.PendingGrantsByGrantee, req.Grantee, req.Pagination,
		func(g types.Grant) bool { return g.Status == types.GrantStatus_GRANT_STATUS_PENDING },
		func(g types.Grant) (types.Grant, error) { return g, nil })
	if err != nil {
		return nil, err
	}
	return &types.QueryPendingGrantsForGranteeResponse{Grants: grants, Pagination: page}, nil
}

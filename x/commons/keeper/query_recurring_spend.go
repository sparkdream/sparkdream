package keeper

import (
	"context"
	"errors"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/commons/types"
	sessiontypes "sparkdream/x/session/types"
)

// GetRecurringSpend returns a single schedule by id.
//
// The query projects from session.Grants. A grant that's been
// cancelled or declined is DELETED from session (terminal-state
// tombstones are intentionally not kept), so a query for such an id
// returns `codes.NotFound`. The audit trail for those terminal
// transitions lives in the `grant_revoked` / `grant_declined`
// events. COMPLETED schedules ARE retained (session keeps the row
// with status flipped to COMPLETED) and are queryable.
func (q queryServer) GetRecurringSpend(ctx context.Context, req *types.QueryGetRecurringSpendRequest) (*types.QueryGetRecurringSpendResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	sk := q.k.SessionKeeper()
	if sk == nil {
		return nil, status.Error(codes.Internal, "session keeper not wired")
	}

	grant, err := sk.GetGrant(ctx, req.Id)
	if err != nil {
		if errors.Is(err, sessiontypes.ErrGrantNotFound) {
			return nil, status.Error(codes.NotFound, "recurring spend not found")
		}
		return nil, status.Error(codes.Internal, err.Error())
	}

	rs, projErr := projectGrantToRecurringSpend(grant)
	if projErr != nil {
		// Not a RECURRING_PULL grant; the id collided with another
		// session grant variant. Treat as NotFound from the commons
		// query surface.
		return nil, status.Error(codes.NotFound, projErr.Error())
	}
	return &types.QueryGetRecurringSpendResponse{RecurringSpend: rs}, nil
}

// ListRecurringSpends paginates schedules by authority OR by recipient.
//
// **M9 (RecurringSpend migration):** the "no filter" pagination path
// (walking the entire RecurringSpends collection) is intentionally
// dropped — the session registry has no efficient cross-granter /
// cross-grantee iterator at the SessionKeeper interface level, and
// "list every recurring spend on-chain" is rarely a useful query
// (callers nearly always filter). At-least-one-filter is now required;
// the request errors with `InvalidArgument` otherwise.
//
// Specifying BOTH authority and recipient remains a hard error
// (neither index obviously wins; callers can intersect client-side).
//
// Pagination uses simple offset+limit over the in-memory slice
// returned by session's ListGrantsBy{Granter,Grantee}. Cursor-based
// pagination (PageRequest.Key) is not supported on this path; PageRequest.{Limit,
// Offset, CountTotal} are honored. The volume of council schedules
// per granter or per recipient is small enough that this is fine.
func (q queryServer) ListRecurringSpends(ctx context.Context, req *types.QueryListRecurringSpendsRequest) (*types.QueryListRecurringSpendsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	if req.Authority != "" && req.Recipient != "" {
		return nil, status.Error(codes.InvalidArgument, "specify at most one of authority or recipient")
	}
	if req.Authority == "" && req.Recipient == "" {
		return nil, status.Error(codes.InvalidArgument,
			"must specify either authority or recipient; cross-granter pagination is not supported post-RecurringSpend-migration")
	}

	sk := q.k.SessionKeeper()
	if sk == nil {
		return nil, status.Error(codes.Internal, "session keeper not wired")
	}

	var grants []sessiontypes.Grant
	var err error
	switch {
	case req.Authority != "":
		grants, err = sk.ListGrantsByGranter(ctx, req.Authority, sessiontypes.GrantType_GRANT_TYPE_RECURRING_PULL)
	case req.Recipient != "":
		grants, err = sk.ListGrantsByGrantee(ctx, req.Recipient, sessiontypes.GrantType_GRANT_TYPE_RECURRING_PULL)
	}
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	// Project each grant. A non-RECURRING_PULL grant should not appear
	// here (we filtered the list call), but skip defensively.
	out := make([]types.RecurringSpend, 0, len(grants))
	for _, g := range grants {
		rs, projErr := projectGrantToRecurringSpend(g)
		if projErr != nil {
			continue
		}
		out = append(out, rs)
	}

	// Manual offset+limit pagination over the projected slice.
	out, pageRes := paginateRecurringSpendsSlice(out, req.Pagination)
	return &types.QueryListRecurringSpendsResponse{RecurringSpends: out, Pagination: pageRes}, nil
}

// projectGrantToRecurringSpend converts a session.Grant carrying a
// RecurringPullPayload back into the commons.RecurringSpend wire shape.
// Returns an error if the grant is not a RECURRING_PULL variant.
//
// `created_via_proposal_id` is always zero post-migration — the proto
// field exists for legacy reasons but is never populated (see plan §4
// and §6 "reserved"). Status maps:
//
//   - GRANT_STATUS_ACTIVE             → RECURRING_SPEND_STATUS_ACTIVE
//   - GRANT_STATUS_PAUSED_INSUFFICIENT_FUNDS → RECURRING_SPEND_STATUS_ACTIVE
//     (PAUSED is observed via grant_paused_underfunded / grant_resumed
//     events; we surface it as ACTIVE here to keep the existing enum
//     values valid — extending the enum is out of scope, see §4.)
//   - GRANT_STATUS_COMPLETED          → RECURRING_SPEND_STATUS_COMPLETED
//
// REVOKED / DECLINED grants are deleted from session and never reach
// this projection.
func projectGrantToRecurringSpend(g sessiontypes.Grant) (types.RecurringSpend, error) {
	if g.Type != sessiontypes.GrantType_GRANT_TYPE_RECURRING_PULL {
		return types.RecurringSpend{}, errors.New("grant is not a RECURRING_PULL variant")
	}
	rp := g.GetRecurringPull()
	if rp == nil {
		return types.RecurringSpend{}, errors.New("recurring_pull payload missing")
	}

	var rsStatus types.RecurringSpendStatus
	switch g.Status {
	case sessiontypes.GrantStatus_GRANT_STATUS_ACTIVE,
		sessiontypes.GrantStatus_GRANT_STATUS_PAUSED_INSUFFICIENT_FUNDS:
		rsStatus = types.RecurringSpendStatus_RECURRING_SPEND_STATUS_ACTIVE
	case sessiontypes.GrantStatus_GRANT_STATUS_COMPLETED:
		rsStatus = types.RecurringSpendStatus_RECURRING_SPEND_STATUS_COMPLETED
	default:
		// Shouldn't reach here (REVOKED/DECLINED grants are deleted);
		// be defensive and treat as ACTIVE so consumers see something
		// well-formed.
		rsStatus = types.RecurringSpendStatus_RECURRING_SPEND_STATUS_ACTIVE
	}

	return types.RecurringSpend{
		Id:                   g.Id,
		Authority:            g.Granter,
		Recipient:            g.Grantee,
		AmountPerPeriod:      sdk.NewCoins(rp.AmountPerPeriod),
		PeriodSeconds:        rp.PeriodSeconds,
		StartTime:            rp.StartTime,
		EndTime:              g.ExpiresAt.UTC().Unix(),
		LastClaimAdvance:     rp.LastClaimAdvance,
		ClaimsMade:           rp.ClaimsMade,
		Status:               rsStatus,
		CreatedViaProposalId: 0, // dead field post-migration; see plan §4
		Note:                 g.Note,
	}, nil
}

// paginateRecurringSpendsSlice applies the limit + offset fields of
// PageRequest to a slice. Cursor-based pagination (PageRequest.Key) is
// ignored — call sites that need it should filter via Authority /
// Recipient and let the typically-small result fit in a single page.
//
// If CountTotal is set, the response Pagination.Total reflects the
// full pre-paginated slice length.
func paginateRecurringSpendsSlice(in []types.RecurringSpend, pr *query.PageRequest) ([]types.RecurringSpend, *query.PageResponse) {
	total := uint64(len(in))
	if pr == nil {
		// Default: return everything up to the SDK page-size default,
		// matching CollectionPaginate's behavior.
		return in, &query.PageResponse{Total: total}
	}

	offset := pr.Offset
	limit := pr.Limit
	if limit == 0 {
		limit = query.DefaultLimit
	}

	if offset >= total {
		return nil, &query.PageResponse{Total: 0}
	}
	end := offset + limit
	if end > total {
		end = total
	}
	out := in[offset:end]

	resp := &query.PageResponse{}
	if pr.CountTotal {
		resp.Total = total
	}
	return out, resp
}


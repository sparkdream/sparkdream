package keeper

import (
	"context"

	"github.com/cosmos/cosmos-sdk/types/query"

	"sparkdream/x/service/types"
)

// checkPagination applies the module's pagination params to a list query
// (§9 Pagination): an omitted limit becomes default_pagination_limit, and a
// limit above max_pagination_limit is rejected with
// ErrPaginationLimitExceeded, so one query cannot walk an unbounded number
// of records. The caller's request is copied, never mutated.
func (k Keeper) checkPagination(ctx context.Context, p *query.PageRequest) (*query.PageRequest, error) {
	params, err := k.Params.Get(ctx)
	if err != nil {
		return p, nil
	}
	out := &query.PageRequest{}
	if p != nil {
		cp := *p
		out = &cp
	}
	if out.Limit == 0 && params.DefaultPaginationLimit > 0 {
		out.Limit = uint64(params.DefaultPaginationLimit)
	}
	if params.MaxPaginationLimit > 0 && out.Limit > uint64(params.MaxPaginationLimit) {
		return nil, types.ErrPaginationLimitExceeded.Wrapf("limit %d > max_pagination_limit %d", out.Limit, params.MaxPaginationLimit)
	}
	return out, nil
}

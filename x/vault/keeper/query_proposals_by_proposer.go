package keeper

import (
	"context"
	"sort"

	"cosmossdk.io/collections"
	"github.com/cosmos/cosmos-sdk/types/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/vault/types"
)

// ProposalsByProposer lists the proposals a member opened under any of its
// x/rep addresses (proposals are stored under the canonical address at the
// time they were opened), optionally filtered by status. It walks the
// ProposalsByProposer prefix of each address, so its cost is the member's
// own retained proposals, never the whole store. Pagination is by offset
// and limit over the merged, id-ordered list (a next_key cannot span
// several prefixes); count_total reports the merged size.
func (q queryServer) ProposalsByProposer(ctx context.Context, req *types.QueryProposalsByProposerRequest) (*types.QueryProposalsByProposerResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	if _, err := q.k.addressCodec.StringToBytes(req.Proposer); err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid address")
	}
	if req.Pagination != nil && len(req.Pagination.Key) > 0 {
		return nil, status.Error(codes.InvalidArgument, "pagination by key is not supported here; use offset and limit")
	}
	var ids []uint64
	for _, addr := range q.k.allAddressesOf(ctx, req.Proposer) {
		rng := collections.NewPrefixedPairRange[string, uint64](addr)
		if err := q.k.ProposalsByProposer.Walk(ctx, rng, func(key collections.Pair[string, uint64]) (bool, error) {
			ids = append(ids, key.K2())
			return false, nil
		}); err != nil {
			return nil, status.Error(codes.Internal, err.Error())
		}
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })

	var matched []types.VaultProposal
	for _, id := range ids {
		p, err := q.k.VaultProposal.Get(ctx, id)
		if err != nil {
			continue
		}
		if req.Status == types.ProposalStatus_PROPOSAL_STATUS_UNSPECIFIED || p.Status == req.Status {
			matched = append(matched, p)
		}
	}

	offset, limit := uint64(0), uint64(query.DefaultLimit)
	countTotal := false
	if req.Pagination != nil {
		offset = req.Pagination.Offset
		if req.Pagination.Limit > 0 {
			limit = req.Pagination.Limit
		}
		countTotal = req.Pagination.CountTotal
	}
	total := uint64(len(matched))
	if offset > total {
		offset = total
	}
	end := offset + limit
	if end > total {
		end = total
	}
	pageRes := &query.PageResponse{}
	if countTotal {
		pageRes.Total = total
	}
	return &types.QueryProposalsByProposerResponse{Proposals: matched[offset:end], Pagination: pageRes}, nil
}

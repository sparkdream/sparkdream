package keeper

import (
	"context"

	"sparkdream/x/rep/types"

	"cosmossdk.io/collections"
	"github.com/cosmos/cosmos-sdk/types/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// JuryReviewsByJuror lists the reviews a juror has been seated on.
//
// This is the discovery half of juror participation. Jury duty pays
// StandardComplexityBudget and is drawn by lot, so it arrives rarely and
// without warning; before this there was no way to ask "am I seated" short of
// paging through every review ever created. An unnoticed summons is the main
// way a jury loses quorum, and a lost quorum used to freeze the initiative
// permanently (see ExpireInterim).
//
// Unlike the other by-address queries in this module, this one reads the
// JuryReviewsByJuror index and pages within that juror's prefix, so cost scales
// with the juror's own seatings rather than with every review on the chain.
func (q queryServer) JuryReviewsByJuror(ctx context.Context, req *types.QueryJuryReviewsByJurorRequest) (*types.QueryJuryReviewsByJurorResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	if req.Juror == "" {
		return nil, status.Error(codes.InvalidArgument, "juror address cannot be empty")
	}

	reviews, pageRes, err := query.CollectionFilteredPaginate(
		ctx,
		q.k.JuryReviewsByJuror,
		req.Pagination,
		func(key collections.Pair[string, uint64], _ collections.NoValue) (bool, error) {
			if !req.PendingOnly {
				return true, nil
			}
			review, err := q.k.JuryReview.Get(ctx, key.K2())
			if err != nil {
				return false, nil
			}
			return review.Verdict == types.Verdict_VERDICT_PENDING, nil
		},
		func(key collections.Pair[string, uint64], _ collections.NoValue) (types.JuryReview, error) {
			return q.k.JuryReview.Get(ctx, key.K2())
		},
		query.WithCollectionPaginationPairPrefix[string, uint64](req.Juror),
	)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	// Seats drawn under an address the juror has since migrated away from are
	// indexed under that address. The page above is the requested address's
	// prefix; the other aliases' seatings are appended in full after it
	// (pagination stays per-alias-naive: the index is prefix-paged, so a
	// cross-alias cursor would have to be invented).
	for _, alias := range q.k.AllAddressesOf(ctx, req.Juror) {
		if alias == req.Juror {
			continue
		}
		rng := collections.NewPrefixedPairRange[string, uint64](alias)
		if err := q.k.JuryReviewsByJuror.Walk(ctx, rng, func(key collections.Pair[string, uint64]) (bool, error) {
			review, gErr := q.k.JuryReview.Get(ctx, key.K2())
			if gErr != nil {
				return false, nil
			}
			if req.PendingOnly && review.Verdict != types.Verdict_VERDICT_PENDING {
				return false, nil
			}
			reviews = append(reviews, review)
			return false, nil
		}); err != nil {
			return nil, status.Error(codes.Internal, err.Error())
		}
	}

	return &types.QueryJuryReviewsByJurorResponse{JuryReview: reviews, Pagination: pageRes}, nil
}

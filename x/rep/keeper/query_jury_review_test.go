package keeper_test

import (
	"context"
	"strconv"
	"testing"

	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/types/query"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

func createNJuryReview(keeper keeper.Keeper, ctx context.Context, n int) []types.JuryReview {
	items := make([]types.JuryReview, n)
	for i := range items {
		iu := uint64(i)
		items[i].Id = iu
		items[i].ChallengeId = uint64(i)
		items[i].InitiativeId = uint64(i)
		items[i].RequiredVotes = uint32(i)
		items[i].ReviewDeliverable = strconv.Itoa(i)
		items[i].ChallengerClaim = strconv.Itoa(i)
		items[i].AssigneeResponse = strconv.Itoa(i)
		items[i].Deadline = int64(i)
		items[i].Verdict = types.Verdict(i)
		items[i].Reasoning = strconv.Itoa(i)
		_ = keeper.JuryReview.Set(ctx, iu, items[i])
		_ = keeper.JuryReviewSeq.Set(ctx, iu)
	}
	return items
}

func TestJuryReviewQuerySingle(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)
	msgs := createNJuryReview(f.keeper, f.ctx, 2)
	tests := []struct {
		desc     string
		request  *types.QueryGetJuryReviewRequest
		response *types.QueryGetJuryReviewResponse
		err      error
	}{
		{
			desc:     "First",
			request:  &types.QueryGetJuryReviewRequest{Id: msgs[0].Id},
			response: &types.QueryGetJuryReviewResponse{JuryReview: msgs[0]},
		},
		{
			desc:     "Second",
			request:  &types.QueryGetJuryReviewRequest{Id: msgs[1].Id},
			response: &types.QueryGetJuryReviewResponse{JuryReview: msgs[1]},
		},
		{
			desc:    "KeyNotFound",
			request: &types.QueryGetJuryReviewRequest{Id: uint64(len(msgs))},
			err:     sdkerrors.ErrKeyNotFound,
		},
		{
			desc: "InvalidRequest",
			err:  status.Error(codes.InvalidArgument, "invalid request"),
		},
	}
	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			response, err := qs.GetJuryReview(f.ctx, tc.request)
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
			} else {
				require.NoError(t, err)
				require.EqualExportedValues(t, tc.response, response)
			}
		})
	}
}

func TestJuryReviewQueryPaginated(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)
	msgs := createNJuryReview(f.keeper, f.ctx, 5)

	request := func(next []byte, offset, limit uint64, total bool) *types.QueryAllJuryReviewRequest {
		return &types.QueryAllJuryReviewRequest{
			Pagination: &query.PageRequest{
				Key:        next,
				Offset:     offset,
				Limit:      limit,
				CountTotal: total,
			},
		}
	}
	t.Run("ByOffset", func(t *testing.T) {
		step := 2
		for i := 0; i < len(msgs); i += step {
			resp, err := qs.ListJuryReview(f.ctx, request(nil, uint64(i), uint64(step), false))
			require.NoError(t, err)
			require.LessOrEqual(t, len(resp.JuryReview), step)
			require.Subset(t, msgs, resp.JuryReview)
		}
	})
	t.Run("ByKey", func(t *testing.T) {
		step := 2
		var next []byte
		for i := 0; i < len(msgs); i += step {
			resp, err := qs.ListJuryReview(f.ctx, request(next, 0, uint64(step), false))
			require.NoError(t, err)
			require.LessOrEqual(t, len(resp.JuryReview), step)
			require.Subset(t, msgs, resp.JuryReview)
			next = resp.Pagination.NextKey
		}
	})
	t.Run("Total", func(t *testing.T) {
		resp, err := qs.ListJuryReview(f.ctx, request(nil, 0, 0, true))
		require.NoError(t, err)
		require.Equal(t, len(msgs), int(resp.Pagination.Total))
		require.EqualExportedValues(t, msgs, resp.JuryReview)
	})
	t.Run("InvalidRequest", func(t *testing.T) {
		_, err := qs.ListJuryReview(f.ctx, nil)
		require.ErrorIs(t, err, status.Error(codes.InvalidArgument, "invalid request"))
	})
}

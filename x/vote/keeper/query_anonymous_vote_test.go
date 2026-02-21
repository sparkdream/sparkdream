package keeper_test

import (
	"context"
	"strconv"
	"testing"

	"github.com/cosmos/cosmos-sdk/types/query"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/vote/keeper"
	"sparkdream/x/vote/types"
)

func createNAnonymousVote(keeper keeper.Keeper, ctx context.Context, n int) []types.AnonymousVote {
	items := make([]types.AnonymousVote, n)
	for i := range items {
		items[i].Index = strconv.Itoa(i)
		items[i].ProposalId = uint64(i)
		items[i].VoteOption = uint32(i)
		items[i].SubmittedAt = int64(i)
		_ = keeper.AnonymousVote.Set(ctx, items[i].Index, items[i])
	}
	return items
}

func TestAnonymousVoteQuerySingle(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)
	msgs := createNAnonymousVote(f.keeper, f.ctx, 2)
	tests := []struct {
		desc     string
		request  *types.QueryGetAnonymousVoteRequest
		response *types.QueryGetAnonymousVoteResponse
		err      error
	}{
		{
			desc: "First",
			request: &types.QueryGetAnonymousVoteRequest{
				Index: msgs[0].Index,
			},
			response: &types.QueryGetAnonymousVoteResponse{AnonymousVote: msgs[0]},
		},
		{
			desc: "Second",
			request: &types.QueryGetAnonymousVoteRequest{
				Index: msgs[1].Index,
			},
			response: &types.QueryGetAnonymousVoteResponse{AnonymousVote: msgs[1]},
		},
		{
			desc: "KeyNotFound",
			request: &types.QueryGetAnonymousVoteRequest{
				Index: strconv.Itoa(100000),
			},
			err: status.Error(codes.NotFound, "not found"),
		},
		{
			desc: "InvalidRequest",
			err:  status.Error(codes.InvalidArgument, "invalid request"),
		},
	}
	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			response, err := qs.GetAnonymousVote(f.ctx, tc.request)
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
			} else {
				require.NoError(t, err)
				require.EqualExportedValues(t, tc.response, response)
			}
		})
	}
}

func TestAnonymousVoteQueryPaginated(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)
	msgs := createNAnonymousVote(f.keeper, f.ctx, 5)

	request := func(next []byte, offset, limit uint64, total bool) *types.QueryAllAnonymousVoteRequest {
		return &types.QueryAllAnonymousVoteRequest{
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
			resp, err := qs.ListAnonymousVote(f.ctx, request(nil, uint64(i), uint64(step), false))
			require.NoError(t, err)
			require.LessOrEqual(t, len(resp.AnonymousVote), step)
			require.Subset(t, msgs, resp.AnonymousVote)
		}
	})
	t.Run("ByKey", func(t *testing.T) {
		step := 2
		var next []byte
		for i := 0; i < len(msgs); i += step {
			resp, err := qs.ListAnonymousVote(f.ctx, request(next, 0, uint64(step), false))
			require.NoError(t, err)
			require.LessOrEqual(t, len(resp.AnonymousVote), step)
			require.Subset(t, msgs, resp.AnonymousVote)
			next = resp.Pagination.NextKey
		}
	})
	t.Run("Total", func(t *testing.T) {
		resp, err := qs.ListAnonymousVote(f.ctx, request(nil, 0, 0, true))
		require.NoError(t, err)
		require.Equal(t, len(msgs), int(resp.Pagination.Total))
		require.EqualExportedValues(t, msgs, resp.AnonymousVote)
	})
	t.Run("InvalidRequest", func(t *testing.T) {
		_, err := qs.ListAnonymousVote(f.ctx, nil)
		require.ErrorIs(t, err, status.Error(codes.InvalidArgument, "invalid request"))
	})
}

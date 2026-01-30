package keeper_test

import (
	"context"
	"strconv"
	"testing"

	"github.com/cosmos/cosmos-sdk/types/query"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/season/keeper"
	"sparkdream/x/season/types"
)

func createNDisplayNameModeration(keeper keeper.Keeper, ctx context.Context, n int) []types.DisplayNameModeration {
	items := make([]types.DisplayNameModeration, n)
	for i := range items {
		items[i].Member = strconv.Itoa(i)
		items[i].RejectedName = strconv.Itoa(i)
		items[i].Reason = strconv.Itoa(i)
		items[i].ModeratedAt = int64(i)
		items[i].Active = true
		items[i].AppealChallengeId = strconv.Itoa(i)
		items[i].AppealedAt = int64(i)
		items[i].AppealSucceeded = true
		_ = keeper.DisplayNameModeration.Set(ctx, items[i].Member, items[i])
	}
	return items
}

func TestDisplayNameModerationQuerySingle(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)
	msgs := createNDisplayNameModeration(f.keeper, f.ctx, 2)
	tests := []struct {
		desc     string
		request  *types.QueryGetDisplayNameModerationRequest
		response *types.QueryGetDisplayNameModerationResponse
		err      error
	}{
		{
			desc: "First",
			request: &types.QueryGetDisplayNameModerationRequest{
				Member: msgs[0].Member,
			},
			response: &types.QueryGetDisplayNameModerationResponse{DisplayNameModeration: msgs[0]},
		},
		{
			desc: "Second",
			request: &types.QueryGetDisplayNameModerationRequest{
				Member: msgs[1].Member,
			},
			response: &types.QueryGetDisplayNameModerationResponse{DisplayNameModeration: msgs[1]},
		},
		{
			desc: "KeyNotFound",
			request: &types.QueryGetDisplayNameModerationRequest{
				Member: strconv.Itoa(100000),
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
			response, err := qs.GetDisplayNameModeration(f.ctx, tc.request)
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
			} else {
				require.NoError(t, err)
				require.EqualExportedValues(t, tc.response, response)
			}
		})
	}
}

func TestDisplayNameModerationQueryPaginated(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)
	msgs := createNDisplayNameModeration(f.keeper, f.ctx, 5)

	request := func(next []byte, offset, limit uint64, total bool) *types.QueryAllDisplayNameModerationRequest {
		return &types.QueryAllDisplayNameModerationRequest{
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
			resp, err := qs.ListDisplayNameModeration(f.ctx, request(nil, uint64(i), uint64(step), false))
			require.NoError(t, err)
			require.LessOrEqual(t, len(resp.DisplayNameModeration), step)
			require.Subset(t, msgs, resp.DisplayNameModeration)
		}
	})
	t.Run("ByKey", func(t *testing.T) {
		step := 2
		var next []byte
		for i := 0; i < len(msgs); i += step {
			resp, err := qs.ListDisplayNameModeration(f.ctx, request(next, 0, uint64(step), false))
			require.NoError(t, err)
			require.LessOrEqual(t, len(resp.DisplayNameModeration), step)
			require.Subset(t, msgs, resp.DisplayNameModeration)
			next = resp.Pagination.NextKey
		}
	})
	t.Run("Total", func(t *testing.T) {
		resp, err := qs.ListDisplayNameModeration(f.ctx, request(nil, 0, 0, true))
		require.NoError(t, err)
		require.Equal(t, len(msgs), int(resp.Pagination.Total))
		require.EqualExportedValues(t, msgs, resp.DisplayNameModeration)
	})
	t.Run("InvalidRequest", func(t *testing.T) {
		_, err := qs.ListDisplayNameModeration(f.ctx, nil)
		require.ErrorIs(t, err, status.Error(codes.InvalidArgument, "invalid request"))
	})
}

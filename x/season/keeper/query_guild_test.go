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

	"sparkdream/x/season/keeper"
	"sparkdream/x/season/types"
)

func createNGuild(keeper keeper.Keeper, ctx context.Context, n int) []types.Guild {
	items := make([]types.Guild, n)
	for i := range items {
		iu := uint64(i)
		items[i].Id = iu
		items[i].Name = strconv.Itoa(i)
		items[i].Description = strconv.Itoa(i)
		items[i].Founder = strconv.Itoa(i)
		items[i].CreatedBlock = int64(i)
		items[i].InviteOnly = true
		items[i].Status = uint64(i)
		_ = keeper.Guild.Set(ctx, iu, items[i])
		_ = keeper.GuildSeq.Set(ctx, iu)
	}
	return items
}

func TestGuildQuerySingle(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)
	msgs := createNGuild(f.keeper, f.ctx, 2)
	tests := []struct {
		desc     string
		request  *types.QueryGetGuildRequest
		response *types.QueryGetGuildResponse
		err      error
	}{
		{
			desc:     "First",
			request:  &types.QueryGetGuildRequest{Id: msgs[0].Id},
			response: &types.QueryGetGuildResponse{Guild: msgs[0]},
		},
		{
			desc:     "Second",
			request:  &types.QueryGetGuildRequest{Id: msgs[1].Id},
			response: &types.QueryGetGuildResponse{Guild: msgs[1]},
		},
		{
			desc:    "KeyNotFound",
			request: &types.QueryGetGuildRequest{Id: uint64(len(msgs))},
			err:     sdkerrors.ErrKeyNotFound,
		},
		{
			desc: "InvalidRequest",
			err:  status.Error(codes.InvalidArgument, "invalid request"),
		},
	}
	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			response, err := qs.GetGuild(f.ctx, tc.request)
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
			} else {
				require.NoError(t, err)
				require.EqualExportedValues(t, tc.response, response)
			}
		})
	}
}

func TestGuildQueryPaginated(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)
	msgs := createNGuild(f.keeper, f.ctx, 5)

	request := func(next []byte, offset, limit uint64, total bool) *types.QueryAllGuildRequest {
		return &types.QueryAllGuildRequest{
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
			resp, err := qs.ListGuild(f.ctx, request(nil, uint64(i), uint64(step), false))
			require.NoError(t, err)
			require.LessOrEqual(t, len(resp.Guild), step)
			require.Subset(t, msgs, resp.Guild)
		}
	})
	t.Run("ByKey", func(t *testing.T) {
		step := 2
		var next []byte
		for i := 0; i < len(msgs); i += step {
			resp, err := qs.ListGuild(f.ctx, request(next, 0, uint64(step), false))
			require.NoError(t, err)
			require.LessOrEqual(t, len(resp.Guild), step)
			require.Subset(t, msgs, resp.Guild)
			next = resp.Pagination.NextKey
		}
	})
	t.Run("Total", func(t *testing.T) {
		resp, err := qs.ListGuild(f.ctx, request(nil, 0, 0, true))
		require.NoError(t, err)
		require.Equal(t, len(msgs), int(resp.Pagination.Total))
		require.EqualExportedValues(t, msgs, resp.Guild)
	})
	t.Run("InvalidRequest", func(t *testing.T) {
		_, err := qs.ListGuild(f.ctx, nil)
		require.ErrorIs(t, err, status.Error(codes.InvalidArgument, "invalid request"))
	})
}

package keeper_test

import (
	"context"
	"fmt"
	"strconv"
	"testing"

	"github.com/cosmos/cosmos-sdk/types/query"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/season/keeper"
	"sparkdream/x/season/types"
)

func createNGuildInvite(keeper keeper.Keeper, ctx context.Context, n int) []types.GuildInvite {
	items := make([]types.GuildInvite, n)
	for i := range items {
		items[i].GuildInvitee = strconv.Itoa(i)
		items[i].Inviter = strconv.Itoa(i)
		items[i].CreatedEpoch = int64(i)
		items[i].ExpiresEpoch = int64(i)
		items[i].GuildId = uint64(i + 1)
		if err := keeper.GuildInvite.Set(ctx, fmt.Sprintf("%d:%s", items[i].GuildId, items[i].GuildInvitee), items[i]); err != nil {
			panic(err)
		}
	}
	return items
}

func TestGuildInviteQuerySingle(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)
	msgs := createNGuildInvite(f.keeper, f.ctx, 2)
	tests := []struct {
		desc     string
		request  *types.QueryGetGuildInviteRequest
		response *types.QueryGetGuildInviteResponse
		err      error
	}{
		{
			desc: "First",
			request: &types.QueryGetGuildInviteRequest{
				GuildInvitee: msgs[0].GuildInvitee,
				GuildId:      msgs[0].GuildId,
			},
			response: &types.QueryGetGuildInviteResponse{GuildInvite: msgs[0]},
		},
		{
			desc: "Second",
			request: &types.QueryGetGuildInviteRequest{
				GuildInvitee: msgs[1].GuildInvitee,
				GuildId:      msgs[1].GuildId,
			},
			response: &types.QueryGetGuildInviteResponse{GuildInvite: msgs[1]},
		},
		{
			desc: "KeyNotFound",
			request: &types.QueryGetGuildInviteRequest{
				GuildInvitee: strconv.Itoa(100000),
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
			response, err := qs.GetGuildInvite(f.ctx, tc.request)
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
			} else {
				require.NoError(t, err)
				require.EqualExportedValues(t, tc.response, response)
			}
		})
	}
}

func TestGuildInviteQueryPaginated(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)
	msgs := createNGuildInvite(f.keeper, f.ctx, 5)

	request := func(next []byte, offset, limit uint64, total bool) *types.QueryAllGuildInviteRequest {
		return &types.QueryAllGuildInviteRequest{
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
			resp, err := qs.ListGuildInvite(f.ctx, request(nil, uint64(i), uint64(step), false))
			require.NoError(t, err)
			require.LessOrEqual(t, len(resp.GuildInvite), step)
			require.Subset(t, msgs, resp.GuildInvite)
		}
	})
	t.Run("ByKey", func(t *testing.T) {
		step := 2
		var next []byte
		for i := 0; i < len(msgs); i += step {
			resp, err := qs.ListGuildInvite(f.ctx, request(next, 0, uint64(step), false))
			require.NoError(t, err)
			require.LessOrEqual(t, len(resp.GuildInvite), step)
			require.Subset(t, msgs, resp.GuildInvite)
			next = resp.Pagination.NextKey
		}
	})
	t.Run("Total", func(t *testing.T) {
		resp, err := qs.ListGuildInvite(f.ctx, request(nil, 0, 0, true))
		require.NoError(t, err)
		require.Equal(t, len(msgs), int(resp.Pagination.Total))
		require.EqualExportedValues(t, msgs, resp.GuildInvite)
	})
	t.Run("InvalidRequest", func(t *testing.T) {
		_, err := qs.ListGuildInvite(f.ctx, nil)
		require.ErrorIs(t, err, status.Error(codes.InvalidArgument, "invalid request"))
	})
}

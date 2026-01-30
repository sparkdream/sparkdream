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

func createNGuildMembership(keeper keeper.Keeper, ctx context.Context, n int) []types.GuildMembership {
	items := make([]types.GuildMembership, n)
	for i := range items {
		items[i].Member = strconv.Itoa(i)
		items[i].GuildId = uint64(i)
		items[i].JoinedEpoch = int64(i)
		items[i].LeftEpoch = int64(i)
		items[i].GuildsJoinedThisSeason = uint64(i)
		_ = keeper.GuildMembership.Set(ctx, items[i].Member, items[i])
	}
	return items
}

func TestGuildMembershipQuerySingle(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)
	msgs := createNGuildMembership(f.keeper, f.ctx, 2)
	tests := []struct {
		desc     string
		request  *types.QueryGetGuildMembershipRequest
		response *types.QueryGetGuildMembershipResponse
		err      error
	}{
		{
			desc: "First",
			request: &types.QueryGetGuildMembershipRequest{
				Member: msgs[0].Member,
			},
			response: &types.QueryGetGuildMembershipResponse{GuildMembership: msgs[0]},
		},
		{
			desc: "Second",
			request: &types.QueryGetGuildMembershipRequest{
				Member: msgs[1].Member,
			},
			response: &types.QueryGetGuildMembershipResponse{GuildMembership: msgs[1]},
		},
		{
			desc: "KeyNotFound",
			request: &types.QueryGetGuildMembershipRequest{
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
			response, err := qs.GetGuildMembership(f.ctx, tc.request)
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
			} else {
				require.NoError(t, err)
				require.EqualExportedValues(t, tc.response, response)
			}
		})
	}
}

func TestGuildMembershipQueryPaginated(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)
	msgs := createNGuildMembership(f.keeper, f.ctx, 5)

	request := func(next []byte, offset, limit uint64, total bool) *types.QueryAllGuildMembershipRequest {
		return &types.QueryAllGuildMembershipRequest{
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
			resp, err := qs.ListGuildMembership(f.ctx, request(nil, uint64(i), uint64(step), false))
			require.NoError(t, err)
			require.LessOrEqual(t, len(resp.GuildMembership), step)
			require.Subset(t, msgs, resp.GuildMembership)
		}
	})
	t.Run("ByKey", func(t *testing.T) {
		step := 2
		var next []byte
		for i := 0; i < len(msgs); i += step {
			resp, err := qs.ListGuildMembership(f.ctx, request(next, 0, uint64(step), false))
			require.NoError(t, err)
			require.LessOrEqual(t, len(resp.GuildMembership), step)
			require.Subset(t, msgs, resp.GuildMembership)
			next = resp.Pagination.NextKey
		}
	})
	t.Run("Total", func(t *testing.T) {
		resp, err := qs.ListGuildMembership(f.ctx, request(nil, 0, 0, true))
		require.NoError(t, err)
		require.Equal(t, len(msgs), int(resp.Pagination.Total))
		require.EqualExportedValues(t, msgs, resp.GuildMembership)
	})
	t.Run("InvalidRequest", func(t *testing.T) {
		_, err := qs.ListGuildMembership(f.ctx, nil)
		require.ErrorIs(t, err, status.Error(codes.InvalidArgument, "invalid request"))
	})
}

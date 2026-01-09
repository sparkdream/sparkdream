package keeper_test

import (
	"context"
	"strconv"
	"testing"

	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/types/query"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

func createNMember(keeper keeper.Keeper, ctx context.Context, n int) []types.Member {
	items := make([]types.Member, n)
	for i := range items {
		items[i].Address = strconv.Itoa(i)
		amount := math.NewInt(int64(i))
		items[i].DreamBalance = &amount
		items[i].StakedDream = &amount
		items[i].LifetimeEarned = &amount
		items[i].LifetimeBurned = &amount
		items[i].TrustLevel = types.TrustLevel(i)
		items[i].JoinedSeason = uint32(i)
		items[i].JoinedAt = int64(i)
		items[i].InvitedBy = strconv.Itoa(i)
		items[i].InvitationCredits = uint32(i)
		items[i].Status = types.MemberStatus(i)
		items[i].ZeroedAt = int64(i)
		items[i].ZeroedCount = uint32(i)
		_ = keeper.Member.Set(ctx, items[i].Address, items[i])
	}
	return items
}

func TestMemberQuerySingle(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)
	msgs := createNMember(f.keeper, f.ctx, 2)
	tests := []struct {
		desc     string
		request  *types.QueryGetMemberRequest
		response *types.QueryGetMemberResponse
		err      error
	}{
		{
			desc: "First",
			request: &types.QueryGetMemberRequest{
				Address: msgs[0].Address,
			},
			response: &types.QueryGetMemberResponse{Member: msgs[0]},
		},
		{
			desc: "Second",
			request: &types.QueryGetMemberRequest{
				Address: msgs[1].Address,
			},
			response: &types.QueryGetMemberResponse{Member: msgs[1]},
		},
		{
			desc: "KeyNotFound",
			request: &types.QueryGetMemberRequest{
				Address: strconv.Itoa(100000),
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
			response, err := qs.GetMember(f.ctx, tc.request)
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
			} else {
				require.NoError(t, err)
				require.EqualExportedValues(t, tc.response, response)
			}
		})
	}
}

func TestMemberQueryPaginated(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)
	msgs := createNMember(f.keeper, f.ctx, 5)

	request := func(next []byte, offset, limit uint64, total bool) *types.QueryAllMemberRequest {
		return &types.QueryAllMemberRequest{
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
			resp, err := qs.ListMember(f.ctx, request(nil, uint64(i), uint64(step), false))
			require.NoError(t, err)
			require.LessOrEqual(t, len(resp.Member), step)
			require.Subset(t, msgs, resp.Member)
		}
	})
	t.Run("ByKey", func(t *testing.T) {
		step := 2
		var next []byte
		for i := 0; i < len(msgs); i += step {
			resp, err := qs.ListMember(f.ctx, request(next, 0, uint64(step), false))
			require.NoError(t, err)
			require.LessOrEqual(t, len(resp.Member), step)
			require.Subset(t, msgs, resp.Member)
			next = resp.Pagination.NextKey
		}
	})
	t.Run("Total", func(t *testing.T) {
		resp, err := qs.ListMember(f.ctx, request(nil, 0, 0, true))
		require.NoError(t, err)
		require.Equal(t, len(msgs), int(resp.Pagination.Total))
		require.EqualExportedValues(t, msgs, resp.Member)
	})
	t.Run("InvalidRequest", func(t *testing.T) {
		_, err := qs.ListMember(f.ctx, nil)
		require.ErrorIs(t, err, status.Error(codes.InvalidArgument, "invalid request"))
	})
}

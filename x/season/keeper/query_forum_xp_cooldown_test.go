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

func createNForumXpCooldown(keeper keeper.Keeper, ctx context.Context, n int) []types.ForumXpCooldown {
	items := make([]types.ForumXpCooldown, n)
	for i := range items {
		items[i].BeneficiaryActor = strconv.Itoa(i)
		items[i].LastGrantedEpoch = int64(i)
		_ = keeper.ForumXpCooldown.Set(ctx, items[i].BeneficiaryActor, items[i])
	}
	return items
}

func TestForumXpCooldownQuerySingle(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)
	msgs := createNForumXpCooldown(f.keeper, f.ctx, 2)
	tests := []struct {
		desc     string
		request  *types.QueryGetForumXpCooldownRequest
		response *types.QueryGetForumXpCooldownResponse
		err      error
	}{
		{
			desc: "First",
			request: &types.QueryGetForumXpCooldownRequest{
				BeneficiaryActor: msgs[0].BeneficiaryActor,
			},
			response: &types.QueryGetForumXpCooldownResponse{ForumXpCooldown: msgs[0]},
		},
		{
			desc: "Second",
			request: &types.QueryGetForumXpCooldownRequest{
				BeneficiaryActor: msgs[1].BeneficiaryActor,
			},
			response: &types.QueryGetForumXpCooldownResponse{ForumXpCooldown: msgs[1]},
		},
		{
			desc: "KeyNotFound",
			request: &types.QueryGetForumXpCooldownRequest{
				BeneficiaryActor: strconv.Itoa(100000),
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
			response, err := qs.GetForumXpCooldown(f.ctx, tc.request)
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
			} else {
				require.NoError(t, err)
				require.EqualExportedValues(t, tc.response, response)
			}
		})
	}
}

func TestForumXpCooldownQueryPaginated(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)
	msgs := createNForumXpCooldown(f.keeper, f.ctx, 5)

	request := func(next []byte, offset, limit uint64, total bool) *types.QueryAllForumXpCooldownRequest {
		return &types.QueryAllForumXpCooldownRequest{
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
			resp, err := qs.ListForumXpCooldown(f.ctx, request(nil, uint64(i), uint64(step), false))
			require.NoError(t, err)
			require.LessOrEqual(t, len(resp.ForumXpCooldown), step)
			require.Subset(t, msgs, resp.ForumXpCooldown)
		}
	})
	t.Run("ByKey", func(t *testing.T) {
		step := 2
		var next []byte
		for i := 0; i < len(msgs); i += step {
			resp, err := qs.ListForumXpCooldown(f.ctx, request(next, 0, uint64(step), false))
			require.NoError(t, err)
			require.LessOrEqual(t, len(resp.ForumXpCooldown), step)
			require.Subset(t, msgs, resp.ForumXpCooldown)
			next = resp.Pagination.NextKey
		}
	})
	t.Run("Total", func(t *testing.T) {
		resp, err := qs.ListForumXpCooldown(f.ctx, request(nil, 0, 0, true))
		require.NoError(t, err)
		require.Equal(t, len(msgs), int(resp.Pagination.Total))
		require.EqualExportedValues(t, msgs, resp.ForumXpCooldown)
	})
	t.Run("InvalidRequest", func(t *testing.T) {
		_, err := qs.ListForumXpCooldown(f.ctx, nil)
		require.ErrorIs(t, err, status.Error(codes.InvalidArgument, "invalid request"))
	})
}

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

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/types"
)

func createNGovActionAppeal(keeper keeper.Keeper, ctx context.Context, n int) []types.GovActionAppeal {
	items := make([]types.GovActionAppeal, n)
	for i := range items {
		iu := uint64(i)
		items[i].Id = iu
		items[i].Appellant = strconv.Itoa(i)
		items[i].ActionType = types.GovActionType(i)
		items[i].ActionTarget = strconv.Itoa(i)
		items[i].OriginalReason = strconv.Itoa(i)
		items[i].AppealReason = strconv.Itoa(i)
		items[i].AppealBond = strconv.Itoa(i)
		items[i].CreatedAt = int64(i)
		items[i].Deadline = int64(i)
		items[i].InitiativeId = uint64(i)
		items[i].Status = types.GovAppealStatus(i)
		items[i].OriginalCategoryId = uint64(i)
		_ = keeper.GovActionAppeal.Set(ctx, iu, items[i])
		_ = keeper.GovActionAppealSeq.Set(ctx, iu)
	}
	return items
}

func TestGovActionAppealQuerySingle(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)
	msgs := createNGovActionAppeal(f.keeper, f.ctx, 2)
	tests := []struct {
		desc     string
		request  *types.QueryGetGovActionAppealRequest
		response *types.QueryGetGovActionAppealResponse
		err      error
	}{
		{
			desc:     "First",
			request:  &types.QueryGetGovActionAppealRequest{Id: msgs[0].Id},
			response: &types.QueryGetGovActionAppealResponse{GovActionAppeal: msgs[0]},
		},
		{
			desc:     "Second",
			request:  &types.QueryGetGovActionAppealRequest{Id: msgs[1].Id},
			response: &types.QueryGetGovActionAppealResponse{GovActionAppeal: msgs[1]},
		},
		{
			desc:    "KeyNotFound",
			request: &types.QueryGetGovActionAppealRequest{Id: uint64(len(msgs))},
			err:     sdkerrors.ErrKeyNotFound,
		},
		{
			desc: "InvalidRequest",
			err:  status.Error(codes.InvalidArgument, "invalid request"),
		},
	}
	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			response, err := qs.GetGovActionAppeal(f.ctx, tc.request)
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
			} else {
				require.NoError(t, err)
				require.EqualExportedValues(t, tc.response, response)
			}
		})
	}
}

func TestGovActionAppealQueryPaginated(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)
	msgs := createNGovActionAppeal(f.keeper, f.ctx, 5)

	request := func(next []byte, offset, limit uint64, total bool) *types.QueryAllGovActionAppealRequest {
		return &types.QueryAllGovActionAppealRequest{
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
			resp, err := qs.ListGovActionAppeal(f.ctx, request(nil, uint64(i), uint64(step), false))
			require.NoError(t, err)
			require.LessOrEqual(t, len(resp.GovActionAppeal), step)
			require.Subset(t, msgs, resp.GovActionAppeal)
		}
	})
	t.Run("ByKey", func(t *testing.T) {
		step := 2
		var next []byte
		for i := 0; i < len(msgs); i += step {
			resp, err := qs.ListGovActionAppeal(f.ctx, request(next, 0, uint64(step), false))
			require.NoError(t, err)
			require.LessOrEqual(t, len(resp.GovActionAppeal), step)
			require.Subset(t, msgs, resp.GovActionAppeal)
			next = resp.Pagination.NextKey
		}
	})
	t.Run("Total", func(t *testing.T) {
		resp, err := qs.ListGovActionAppeal(f.ctx, request(nil, 0, 0, true))
		require.NoError(t, err)
		require.Equal(t, len(msgs), int(resp.Pagination.Total))
		require.EqualExportedValues(t, msgs, resp.GovActionAppeal)
	})
	t.Run("InvalidRequest", func(t *testing.T) {
		_, err := qs.ListGovActionAppeal(f.ctx, nil)
		require.ErrorIs(t, err, status.Error(codes.InvalidArgument, "invalid request"))
	})
}

package keeper_test

import (
	"context"
	"strconv"
	"testing"

	"cosmossdk.io/math"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/types/query"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

func createNProject(keeper keeper.Keeper, ctx context.Context, n int) []types.Project {
	items := make([]types.Project, n)
	for i := range items {
		iu := uint64(i)
		items[i].Id = iu
		items[i].Name = strconv.Itoa(i)
		items[i].Description = strconv.Itoa(i)
		items[i].Creator = strconv.Itoa(i)
		items[i].Category = types.ProjectCategory(i)
		items[i].Council = strconv.Itoa(i)
		amount := math.NewInt(int64(i))
		items[i].ApprovedBudget = &amount
		items[i].AllocatedBudget = &amount
		items[i].SpentBudget = &amount
		items[i].ApprovedSpark = &amount
		items[i].SpentSpark = &amount
		items[i].Status = types.ProjectStatus(i)
		items[i].ApprovedBy = strconv.Itoa(i)
		items[i].ApprovedAt = int64(i)
		items[i].CompletedAt = int64(i)
		_ = keeper.Project.Set(ctx, iu, items[i])
		_ = keeper.ProjectSeq.Set(ctx, iu)
	}
	return items
}

func TestProjectQuerySingle(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)
	msgs := createNProject(f.keeper, f.ctx, 2)
	tests := []struct {
		desc     string
		request  *types.QueryGetProjectRequest
		response *types.QueryGetProjectResponse
		err      error
	}{
		{
			desc:     "First",
			request:  &types.QueryGetProjectRequest{Id: msgs[0].Id},
			response: &types.QueryGetProjectResponse{Project: msgs[0]},
		},
		{
			desc:     "Second",
			request:  &types.QueryGetProjectRequest{Id: msgs[1].Id},
			response: &types.QueryGetProjectResponse{Project: msgs[1]},
		},
		{
			desc:    "KeyNotFound",
			request: &types.QueryGetProjectRequest{Id: uint64(len(msgs))},
			err:     sdkerrors.ErrKeyNotFound,
		},
		{
			desc: "InvalidRequest",
			err:  status.Error(codes.InvalidArgument, "invalid request"),
		},
	}
	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			response, err := qs.GetProject(f.ctx, tc.request)
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
			} else {
				require.NoError(t, err)
				require.EqualExportedValues(t, tc.response, response)
			}
		})
	}
}

func TestProjectQueryPaginated(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)
	msgs := createNProject(f.keeper, f.ctx, 5)

	request := func(next []byte, offset, limit uint64, total bool) *types.QueryAllProjectRequest {
		return &types.QueryAllProjectRequest{
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
			resp, err := qs.ListProject(f.ctx, request(nil, uint64(i), uint64(step), false))
			require.NoError(t, err)
			require.LessOrEqual(t, len(resp.Project), step)
			require.Subset(t, msgs, resp.Project)
		}
	})
	t.Run("ByKey", func(t *testing.T) {
		step := 2
		var next []byte
		for i := 0; i < len(msgs); i += step {
			resp, err := qs.ListProject(f.ctx, request(next, 0, uint64(step), false))
			require.NoError(t, err)
			require.LessOrEqual(t, len(resp.Project), step)
			require.Subset(t, msgs, resp.Project)
			next = resp.Pagination.NextKey
		}
	})
	t.Run("Total", func(t *testing.T) {
		resp, err := qs.ListProject(f.ctx, request(nil, 0, 0, true))
		require.NoError(t, err)
		require.Equal(t, len(msgs), int(resp.Pagination.Total))
		require.EqualExportedValues(t, msgs, resp.Project)
	})
	t.Run("InvalidRequest", func(t *testing.T) {
		_, err := qs.ListProject(f.ctx, nil)
		require.ErrorIs(t, err, status.Error(codes.InvalidArgument, "invalid request"))
	})
}

package keeper_test

import (
	"context"
	"strconv"
	"testing"

	"github.com/cosmos/cosmos-sdk/types/query"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

func createNInterimTemplate(keeper keeper.Keeper, ctx context.Context, n int) []types.InterimTemplate {
	items := make([]types.InterimTemplate, n)
	for i := range items {
		items[i].Id = strconv.Itoa(i)
		items[i].Name = strconv.Itoa(i)
		items[i].VerificationGuide = strconv.Itoa(i)
		_ = keeper.InterimTemplate.Set(ctx, items[i].Id, items[i])
	}
	return items
}

func TestInterimTemplateQuerySingle(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)
	msgs := createNInterimTemplate(f.keeper, f.ctx, 2)
	tests := []struct {
		desc     string
		request  *types.QueryGetInterimTemplateRequest
		response *types.QueryGetInterimTemplateResponse
		err      error
	}{
		{
			desc: "First",
			request: &types.QueryGetInterimTemplateRequest{
				TemplateId: msgs[0].Id,
			},
			response: &types.QueryGetInterimTemplateResponse{InterimTemplate: msgs[0]},
		},
		{
			desc: "Second",
			request: &types.QueryGetInterimTemplateRequest{
				TemplateId: msgs[1].Id,
			},
			response: &types.QueryGetInterimTemplateResponse{InterimTemplate: msgs[1]},
		},
		{
			desc: "KeyNotFound",
			request: &types.QueryGetInterimTemplateRequest{
				TemplateId: strconv.Itoa(100000),
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
			response, err := qs.GetInterimTemplate(f.ctx, tc.request)
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
			} else {
				require.NoError(t, err)
				require.EqualExportedValues(t, tc.response, response)
			}
		})
	}
}

func TestInterimTemplateQueryPaginated(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)
	msgs := createNInterimTemplate(f.keeper, f.ctx, 5)

	request := func(next []byte, offset, limit uint64, total bool) *types.QueryAllInterimTemplateRequest {
		return &types.QueryAllInterimTemplateRequest{
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
			resp, err := qs.ListInterimTemplate(f.ctx, request(nil, uint64(i), uint64(step), false))
			require.NoError(t, err)
			require.LessOrEqual(t, len(resp.InterimTemplate), step)
			require.Subset(t, msgs, resp.InterimTemplate)
		}
	})
	t.Run("ByKey", func(t *testing.T) {
		step := 2
		var next []byte
		for i := 0; i < len(msgs); i += step {
			resp, err := qs.ListInterimTemplate(f.ctx, request(next, 0, uint64(step), false))
			require.NoError(t, err)
			require.LessOrEqual(t, len(resp.InterimTemplate), step)
			require.Subset(t, msgs, resp.InterimTemplate)
			next = resp.Pagination.NextKey
		}
	})
	t.Run("Total", func(t *testing.T) {
		resp, err := qs.ListInterimTemplate(f.ctx, request(nil, 0, 0, true))
		require.NoError(t, err)
		require.Equal(t, len(msgs), int(resp.Pagination.Total))
		require.EqualExportedValues(t, msgs, resp.InterimTemplate)
	})
	t.Run("InvalidRequest", func(t *testing.T) {
		_, err := qs.ListInterimTemplate(f.ctx, nil)
		require.ErrorIs(t, err, status.Error(codes.InvalidArgument, "invalid request"))
	})
}

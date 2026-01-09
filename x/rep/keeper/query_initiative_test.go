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

func createNInitiative(keeper keeper.Keeper, ctx context.Context, n int) []types.Initiative {
	items := make([]types.Initiative, n)
	for i := range items {
		iu := uint64(i)
		items[i].Id = iu
		items[i].ProjectId = uint64(i)
		items[i].Title = strconv.Itoa(i)
		items[i].Description = strconv.Itoa(i)
		items[i].Tier = types.InitiativeTier(i)
		items[i].Category = types.InitiativeCategory(i)
		items[i].TemplateId = strconv.Itoa(i)
		amount := math.NewInt(int64(i))
		items[i].Budget = &amount
		items[i].Assignee = strconv.Itoa(i)
		items[i].Apprentice = strconv.Itoa(i)
		items[i].AssignedAt = int64(i)
		items[i].DeliverableUri = strconv.Itoa(i)
		items[i].SubmittedAt = int64(i)
		dec := math.LegacyNewDec(int64(i))
		items[i].RequiredConviction = &dec
		items[i].CurrentConviction = &dec
		items[i].ExternalConviction = &dec
		items[i].ConvictionLastUpdated = int64(i)
		items[i].ReviewPeriodEnd = int64(i)
		items[i].ChallengePeriodEnd = int64(i)
		items[i].Status = types.InitiativeStatus(i)
		items[i].CreatedAt = int64(i)
		items[i].CompletedAt = int64(i)
		_ = keeper.Initiative.Set(ctx, iu, items[i])
		_ = keeper.InitiativeSeq.Set(ctx, iu)
	}
	return items
}

func TestInitiativeQuerySingle(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)
	msgs := createNInitiative(f.keeper, f.ctx, 2)
	tests := []struct {
		desc     string
		request  *types.QueryGetInitiativeRequest
		response *types.QueryGetInitiativeResponse
		err      error
	}{
		{
			desc:     "First",
			request:  &types.QueryGetInitiativeRequest{Id: msgs[0].Id},
			response: &types.QueryGetInitiativeResponse{Initiative: msgs[0]},
		},
		{
			desc:     "Second",
			request:  &types.QueryGetInitiativeRequest{Id: msgs[1].Id},
			response: &types.QueryGetInitiativeResponse{Initiative: msgs[1]},
		},
		{
			desc:    "KeyNotFound",
			request: &types.QueryGetInitiativeRequest{Id: uint64(len(msgs))},
			err:     sdkerrors.ErrKeyNotFound,
		},
		{
			desc: "InvalidRequest",
			err:  status.Error(codes.InvalidArgument, "invalid request"),
		},
	}
	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			response, err := qs.GetInitiative(f.ctx, tc.request)
			if tc.err != nil {
				require.ErrorIs(t, err, tc.err)
			} else {
				require.NoError(t, err)
				require.EqualExportedValues(t, tc.response, response)
			}
		})
	}
}

func TestInitiativeQueryPaginated(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)
	msgs := createNInitiative(f.keeper, f.ctx, 5)

	request := func(next []byte, offset, limit uint64, total bool) *types.QueryAllInitiativeRequest {
		return &types.QueryAllInitiativeRequest{
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
			resp, err := qs.ListInitiative(f.ctx, request(nil, uint64(i), uint64(step), false))
			require.NoError(t, err)
			require.LessOrEqual(t, len(resp.Initiative), step)
			require.Subset(t, msgs, resp.Initiative)
		}
	})
	t.Run("ByKey", func(t *testing.T) {
		step := 2
		var next []byte
		for i := 0; i < len(msgs); i += step {
			resp, err := qs.ListInitiative(f.ctx, request(next, 0, uint64(step), false))
			require.NoError(t, err)
			require.LessOrEqual(t, len(resp.Initiative), step)
			require.Subset(t, msgs, resp.Initiative)
			next = resp.Pagination.NextKey
		}
	})
	t.Run("Total", func(t *testing.T) {
		resp, err := qs.ListInitiative(f.ctx, request(nil, 0, 0, true))
		require.NoError(t, err)
		require.Equal(t, len(msgs), int(resp.Pagination.Total))
		require.EqualExportedValues(t, msgs, resp.Initiative)
	})
	t.Run("InvalidRequest", func(t *testing.T) {
		_, err := qs.ListInitiative(f.ctx, nil)
		require.ErrorIs(t, err, status.Error(codes.InvalidArgument, "invalid request"))
	})
}

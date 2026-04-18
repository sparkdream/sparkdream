package keeper_test

import (
	"testing"

	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/types/query"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

func TestQueryGovActionAppeal(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	appeals := []types.GovActionAppeal{
		{Id: 1, Appellant: "phoenix", ActionType: types.GovActionType_GOV_ACTION_TYPE_WARNING, ActionTarget: "t1", Status: types.GovAppealStatus_GOV_APPEAL_STATUS_PENDING},
		{Id: 2, Appellant: "aurora", ActionType: types.GovActionType_GOV_ACTION_TYPE_DEMOTION, ActionTarget: "t2", Status: types.GovAppealStatus_GOV_APPEAL_STATUS_UPHELD},
		{Id: 3, Appellant: "zenith", ActionType: types.GovActionType_GOV_ACTION_TYPE_ZEROING, ActionTarget: "t3", Status: types.GovAppealStatus_GOV_APPEAL_STATUS_OVERTURNED},
	}
	for _, a := range appeals {
		require.NoError(t, f.keeper.GovActionAppeal.Set(f.ctx, a.Id, a))
	}

	t.Run("get found", func(t *testing.T) {
		resp, err := qs.GetGovActionAppeal(f.ctx, &types.QueryGetGovActionAppealRequest{Id: 2})
		require.NoError(t, err)
		require.Equal(t, "aurora", resp.GovActionAppeal.Appellant)
		require.Equal(t, types.GovActionType_GOV_ACTION_TYPE_DEMOTION, resp.GovActionAppeal.ActionType)
	})

	t.Run("get not found", func(t *testing.T) {
		_, err := qs.GetGovActionAppeal(f.ctx, &types.QueryGetGovActionAppealRequest{Id: 999})
		require.Error(t, err)
		require.ErrorIs(t, err, sdkerrors.ErrKeyNotFound)
	})

	t.Run("get nil request", func(t *testing.T) {
		_, err := qs.GetGovActionAppeal(f.ctx, nil)
		require.Error(t, err)
		require.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("list all", func(t *testing.T) {
		resp, err := qs.ListGovActionAppeal(f.ctx, &types.QueryAllGovActionAppealRequest{
			Pagination: &query.PageRequest{CountTotal: true},
		})
		require.NoError(t, err)
		require.Len(t, resp.GovActionAppeal, 3)
		require.Equal(t, uint64(3), resp.Pagination.Total)
	})

	t.Run("list paginated", func(t *testing.T) {
		resp, err := qs.ListGovActionAppeal(f.ctx, &types.QueryAllGovActionAppealRequest{
			Pagination: &query.PageRequest{Limit: 1, CountTotal: true},
		})
		require.NoError(t, err)
		require.Len(t, resp.GovActionAppeal, 1)
		require.NotEmpty(t, resp.Pagination.NextKey)
	})

	t.Run("list nil request", func(t *testing.T) {
		_, err := qs.ListGovActionAppeal(f.ctx, nil)
		require.Error(t, err)
		require.Equal(t, codes.InvalidArgument, status.Code(err))
	})
}

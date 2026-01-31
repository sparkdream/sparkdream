package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/types"
)

func TestQueryGovActionAppeals(t *testing.T) {
	f := initFixture(t)
	qs := keeper.NewQueryServerImpl(f.keeper)

	t.Run("nil request", func(t *testing.T) {
		_, err := qs.GovActionAppeals(f.ctx, nil)
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		require.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("no appeals", func(t *testing.T) {
		resp, err := qs.GovActionAppeals(f.ctx, &types.QueryGovActionAppealsRequest{})
		require.NoError(t, err)
		require.Equal(t, uint64(0), resp.AppealId)
	})

	t.Run("has appeals", func(t *testing.T) {
		now := f.sdkCtx().BlockTime().Unix()

		// Create appeal
		appeal := types.GovActionAppeal{
			Id:           1,
			Appellant:    testCreator,
			ActionType:   types.GovActionType_GOV_ACTION_TYPE_WARNING,
			AppealReason: "unjust warning",
			CreatedAt:    now,
			Status:       types.GovAppealStatus_GOV_APPEAL_STATUS_PENDING,
		}
		f.keeper.GovActionAppeal.Set(f.ctx, 1, appeal)

		resp, err := qs.GovActionAppeals(f.ctx, &types.QueryGovActionAppealsRequest{})
		require.NoError(t, err)
		require.Equal(t, uint64(1), resp.AppealId)
	})
}

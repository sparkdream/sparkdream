package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/types"
)

func TestMsgAppealGovAction(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)

	t.Run("invalid creator address", func(t *testing.T) {
		_, err := ms.AppealGovAction(f.ctx, &types.MsgAppealGovAction{
			Creator:      "invalid",
			ActionType:   uint64(types.GovActionType_GOV_ACTION_TYPE_WARNING),
			ActionTarget: testCreator2,
			AppealReason: "unjust warning",
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid creator address")
	})

	t.Run("invalid action type", func(t *testing.T) {
		_, err := ms.AppealGovAction(f.ctx, &types.MsgAppealGovAction{
			Creator:      testCreator,
			ActionType:   0, // Unspecified
			ActionTarget: testCreator2,
			AppealReason: "unjust action",
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid action type")
	})

	t.Run("success", func(t *testing.T) {
		_, err := ms.AppealGovAction(f.ctx, &types.MsgAppealGovAction{
			Creator:      testCreator,
			ActionType:   uint64(types.GovActionType_GOV_ACTION_TYPE_WARNING),
			ActionTarget: testCreator2,
			AppealReason: "unjust warning",
		})
		require.NoError(t, err)

		// Verify appeal was created
		iter, err := f.keeper.GovActionAppeal.Iterate(f.ctx, nil)
		require.NoError(t, err)
		defer iter.Close()

		var found bool
		for ; iter.Valid(); iter.Next() {
			appeal, _ := iter.Value()
			if appeal.Appellant == testCreator && appeal.ActionType == types.GovActionType_GOV_ACTION_TYPE_WARNING {
				found = true
				require.Equal(t, testCreator2, appeal.ActionTarget)
				require.Equal(t, "unjust warning", appeal.AppealReason)
				require.Equal(t, types.GovAppealStatus_GOV_APPEAL_STATUS_PENDING, appeal.Status)
				break
			}
		}
		require.True(t, found)
	})

	t.Run("duplicate appeal", func(t *testing.T) {
		// First appeal was already created above
		_, err := ms.AppealGovAction(f.ctx, &types.MsgAppealGovAction{
			Creator:      testCreator,
			ActionType:   uint64(types.GovActionType_GOV_ACTION_TYPE_WARNING),
			ActionTarget: testCreator2,
			AppealReason: "another appeal",
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "appeal already exists")
	})
}

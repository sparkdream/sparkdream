package keeper_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

func TestMsgServerSubmitInterimWork(t *testing.T) {
	t.Run("invalid creator address", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		_, err := ms.SubmitInterimWork(f.ctx, &types.MsgSubmitInterimWork{
			Creator:        "invalid-address",
			InterimId:      1,
			DeliverableUri: "uri",
			Comments:       "Done",
		})

		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid assignee address")
	})

	t.Run("non-existent interim", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		creator := sdk.AccAddress([]byte("creator"))
		creatorStr, err := f.addressCodec.BytesToString(creator)
		require.NoError(t, err)

		_, err = ms.SubmitInterimWork(f.ctx, &types.MsgSubmitInterimWork{
			Creator:        creatorStr,
			InterimId:      99999,
			DeliverableUri: "uri",
			Comments:       "Done",
		})

		require.Error(t, err)
		require.Contains(t, err.Error(), "collections: not found")
	})

	t.Run("successful submission", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)
		k := f.keeper
		ctx := f.ctx

		// Setup: create interim and assignee
		assignee := sdk.AccAddress([]byte("assignee"))
		assigneeStr, err := f.addressCodec.BytesToString(assignee)
		require.NoError(t, err)

		interimID, err := k.CreateInterimWork(
			ctx,
			types.InterimType_INTERIM_TYPE_CONTRIBUTION_REVIEW,
			[]string{assigneeStr},
			"",
			1,
			"initiative",
			types.InterimComplexity_INTERIM_COMPLEXITY_STANDARD,
			12345,
		)
		require.NoError(t, err)

		// Submit work
		_, err = ms.SubmitInterimWork(ctx, &types.MsgSubmitInterimWork{
			Creator:        assigneeStr,
			InterimId:      interimID,
			DeliverableUri: "https://github.com/repo/pull/1",
			Comments:       "Completed review tasks",
		})
		require.NoError(t, err)

		// Verify interim status
		interim, err := k.GetInterim(ctx, interimID)
		require.NoError(t, err)
		require.Equal(t, types.InterimStatus_INTERIM_STATUS_IN_PROGRESS, interim.Status)
	})
}

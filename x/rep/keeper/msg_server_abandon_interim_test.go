package keeper_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

func TestMsgServerAbandonInterim(t *testing.T) {
	t.Run("invalid creator address", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		_, err := ms.AbandonInterim(f.ctx, &types.MsgAbandonInterim{
			Creator:  "invalid-address",
			InterimId: 1,
			Reason:    "Test",
		})

		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid creator address")
	})

	t.Run("non-existent interim", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		assignee := sdk.AccAddress([]byte("assignee"))
		assigneeStr, err := f.addressCodec.BytesToString(assignee)
		require.NoError(t, err)

		_, err = ms.AbandonInterim(f.ctx, &types.MsgAbandonInterim{
			Creator:  assigneeStr,
			InterimId: 99999,
			Reason:    "Test",
		})

		require.Error(t, err)
		require.Contains(t, err.Error(), "failed to get interim")
	})

	t.Run("not an assignee", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)
		k := f.keeper
		ctx := f.ctx

		// Setup: create interim with one assignee
		assignee1 := sdk.AccAddress([]byte("assignee1"))
		assignee1Str, err := f.addressCodec.BytesToString(assignee1)
		require.NoError(t, err)

		interimID, err := k.CreateInterimWork(
			ctx,
			types.InterimType_INTERIM_TYPE_CONTRIBUTION_REVIEW,
			[]string{assignee1Str},
			"",
			1,
			"initiative",
			types.InterimComplexity_INTERIM_COMPLEXITY_STANDARD,
			12345,
		)
		require.NoError(t, err)

		// Try to abandon with different user
		other := sdk.AccAddress([]byte("other"))
		otherStr, err := f.addressCodec.BytesToString(other)
		require.NoError(t, err)

		_, err = ms.AbandonInterim(ctx, &types.MsgAbandonInterim{
			Creator:  otherStr,
			InterimId: interimID,
			Reason:    "Test",
		})

		require.Error(t, err)
		require.Contains(t, err.Error(), "only assignee can abandon interim")
	})

	t.Run("successful abandon - by assignee", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)
		k := f.keeper
		ctx := f.ctx

		// Setup: create interim with assignee
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

		// Abandon interim
		_, err = ms.AbandonInterim(ctx, &types.MsgAbandonInterim{
			Creator:  assigneeStr,
			InterimId: interimID,
			Reason:    "No longer needed",
		})
		require.NoError(t, err)

		// Verify interim status
		interim, err := k.GetInterim(ctx, interimID)
		require.NoError(t, err)
		require.Equal(t, types.InterimStatus_INTERIM_STATUS_EXPIRED, interim.Status)
		require.Equal(t, "No longer needed", interim.CompletionNotes)
	})
}

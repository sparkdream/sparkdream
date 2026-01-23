package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

func TestMsgServerAssignInterim(t *testing.T) {
	t.Run("invalid creator address", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		_, err := ms.AssignInterim(f.ctx, &types.MsgAssignInterim{
			Creator:   "invalid-address",
			InterimId: 1,
			Assignee:  "assignee",
		})

		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid creator address")
	})

	t.Run("invalid assignee address", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		creator := sdk.AccAddress([]byte("creator"))
		creatorStr, err := f.addressCodec.BytesToString(creator)
		require.NoError(t, err)

		_, err = ms.AssignInterim(f.ctx, &types.MsgAssignInterim{
			Creator:   creatorStr,
			InterimId: 1,
			Assignee:  "invalid-address",
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

		assignee := sdk.AccAddress([]byte("assignee"))
		assigneeStr, err := f.addressCodec.BytesToString(assignee)
		require.NoError(t, err)

		_, err = ms.AssignInterim(f.ctx, &types.MsgAssignInterim{
			Creator:   creatorStr,
			InterimId: 99999,
			Assignee:  assigneeStr,
		})

		require.Error(t, err)
		require.Contains(t, err.Error(), "failed to assign interim")
	})

	t.Run("successful assignment", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)
		k := f.keeper
		ctx := f.ctx

		// Setup: create interim
		creator := sdk.AccAddress([]byte("creator"))
		creatorStr, err := f.addressCodec.BytesToString(creator)
		require.NoError(t, err)

		interimID, err := k.CreateInterimWork(
			ctx,
			types.InterimType_INTERIM_TYPE_CONTRIBUTION_REVIEW,
			[]string{},
			"",
			1,
			"initiative",
			types.InterimComplexity_INTERIM_COMPLEXITY_STANDARD,
			12345,
		)
		require.NoError(t, err)

		assignee := sdk.AccAddress([]byte("assignee"))
		assigneeStr, err := f.addressCodec.BytesToString(assignee)
		require.NoError(t, err)

		k.Member.Set(ctx, assigneeStr, types.Member{
			Address:          assigneeStr,
			DreamBalance:     keeper.PtrInt(math.ZeroInt()),
			StakedDream:      keeper.PtrInt(math.ZeroInt()),
			LifetimeEarned:   keeper.PtrInt(math.ZeroInt()),
			LifetimeBurned:   keeper.PtrInt(math.ZeroInt()),
			ReputationScores: map[string]string{"tag": "100.0"},
		})

		// Assign interim
		_, err = ms.AssignInterim(ctx, &types.MsgAssignInterim{
			Creator:   creatorStr,
			InterimId: interimID,
			Assignee:  assigneeStr,
		})
		require.NoError(t, err)

		// Verify interim has assignee
		interim, err := k.GetInterim(ctx, interimID)
		require.NoError(t, err)
		require.Len(t, interim.Assignees, 1)
		require.Equal(t, assigneeStr, interim.Assignees[0])
	})
}

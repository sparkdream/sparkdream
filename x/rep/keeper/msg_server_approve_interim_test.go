package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

func TestMsgServerApproveInterim(t *testing.T) {
	t.Run("invalid creator address", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		_, err := ms.ApproveInterim(f.ctx, &types.MsgApproveInterim{
			Creator:   "invalid-address",
			InterimId: 1,
			Approved:  true,
			Comments:  "LGTM",
		})

		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid approver address")
	})

	t.Run("successful approval", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)
		k := f.keeper
		ctx := f.ctx

		// Setup: create interim, assignee, and work
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
		err = k.SubmitInterimWork(ctx, interimID, assignee, "uri", "notes")
		require.NoError(t, err)

		creator := sdk.AccAddress([]byte("creator"))
		creatorStr, err := f.addressCodec.BytesToString(creator)
		require.NoError(t, err)

		// Approve interim
		_, err = ms.ApproveInterim(ctx, &types.MsgApproveInterim{
			Creator:   creatorStr,
			InterimId: interimID,
			Approved:  true,
			Comments:  "Great work",
		})
		require.NoError(t, err)

		// Verify interim status
		interim, err := k.GetInterim(ctx, interimID)
		require.NoError(t, err)
		require.Equal(t, types.InterimStatus_INTERIM_STATUS_COMPLETED, interim.Status)
	})
}

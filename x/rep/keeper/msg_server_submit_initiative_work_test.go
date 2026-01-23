package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

func TestMsgServerSubmitInitiativeWork(t *testing.T) {
	t.Run("invalid creator address", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		_, err := ms.SubmitInitiativeWork(f.ctx, &types.MsgSubmitInitiativeWork{
			Creator:        "invalid-address",
			InitiativeId:   1,
			DeliverableUri: "uri",
			Comments:       "Done",
		})

		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid creator address")
	})

	t.Run("non-existent initiative", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		creator := sdk.AccAddress([]byte("creator"))
		creatorStr, err := f.addressCodec.BytesToString(creator)
		require.NoError(t, err)

		_, err = ms.SubmitInitiativeWork(f.ctx, &types.MsgSubmitInitiativeWork{
			Creator:        creatorStr,
			InitiativeId:   99999,
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

		// Setup: create project, member, and initiative
		creator := sdk.AccAddress([]byte("creator"))
		k.Member.Set(ctx, creator.String(), types.Member{
			Address:          creator.String(),
			DreamBalance:     PtrInt(math.ZeroInt()),
			StakedDream:      PtrInt(math.ZeroInt()),
			LifetimeEarned:   PtrInt(math.ZeroInt()),
			LifetimeBurned:   PtrInt(math.ZeroInt()),
			ReputationScores: map[string]string{"tag": "100.0"},
		})

		projectID, _ := k.CreateProject(ctx, creator, "Proj", "Desc", []string{"tag"}, types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "technical", math.NewInt(10000), math.NewInt(1000))
		k.ApproveProject(ctx, projectID, sdk.AccAddress([]byte("approver")), math.NewInt(10000), math.NewInt(1000))

		initID, _ := k.CreateInitiative(ctx, creator, projectID, "Task", "D", []string{"tag"}, types.InitiativeTier_INITIATIVE_TIER_STANDARD, types.InitiativeCategory_INITIATIVE_CATEGORY_FEATURE, "", math.NewInt(100))

		assignee := sdk.AccAddress([]byte("assignee"))
		k.Member.Set(ctx, assignee.String(), types.Member{
			Address:          assignee.String(),
			DreamBalance:     PtrInt(math.ZeroInt()),
			StakedDream:      PtrInt(math.ZeroInt()),
			LifetimeEarned:   PtrInt(math.ZeroInt()),
			LifetimeBurned:   PtrInt(math.ZeroInt()),
			ReputationScores: map[string]string{"tag": "100.0"},
		})
		err := k.AssignInitiativeToMember(ctx, initID, assignee)
		require.NoError(t, err)

		assigneeStr, err := f.addressCodec.BytesToString(assignee)
		require.NoError(t, err)

		// Submit work
		_, err = ms.SubmitInitiativeWork(ctx, &types.MsgSubmitInitiativeWork{
			Creator:        assigneeStr,
			InitiativeId:   initID,
			DeliverableUri: "https://github.com/repo/pull/1",
			Comments:       "Implemented feature with tests",
		})
		require.NoError(t, err)

		// Verify initiative status
		initiative, err := k.GetInitiative(ctx, initID)
		require.NoError(t, err)
		require.Equal(t, types.InitiativeStatus_INITIATIVE_STATUS_SUBMITTED, initiative.Status)
	})
}

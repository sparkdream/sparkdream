package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

func TestMsgServerCreateInterim(t *testing.T) {
	t.Run("invalid creator address", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		_, err := ms.CreateInterim(f.ctx, &types.MsgCreateInterim{
			Creator:       "invalid-address",
			InterimType:   1,
			ReferenceId:   1,
			ReferenceType: "initiative",
			Complexity:    1,
			Deadline:      12345,
		})

		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid creator address")
	})

	t.Run("successful creation", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)
		k := f.keeper
		ctx := f.ctx

		// Setup: create project and initiative
		creator := sdk.AccAddress([]byte("creator"))
		k.Member.Set(ctx, creator.String(), types.Member{
			Address:          creator.String(),
			DreamBalance:     PtrInt(math.NewInt(10000)),
			StakedDream:      PtrInt(math.ZeroInt()),
			LifetimeEarned:   PtrInt(math.ZeroInt()),
			LifetimeBurned:   PtrInt(math.ZeroInt()),
			ReputationScores: map[string]string{"tag": "100.0"},
		})

		projectID, _ := k.CreateProject(ctx, creator, "Proj", "Desc", []string{"tag"}, types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "technical", math.NewInt(10000), math.NewInt(1000))
		k.ApproveProject(ctx, projectID, sdk.AccAddress([]byte("approver")), math.NewInt(10000), math.NewInt(1000))
		initID, _ := k.CreateInitiative(ctx, creator, projectID, "Task", "D", []string{"tag"}, types.InitiativeTier_INITIATIVE_TIER_STANDARD, types.InitiativeCategory_INITIATIVE_CATEGORY_FEATURE, "", math.NewInt(100))

		creatorStr, err := f.addressCodec.BytesToString(creator)
		require.NoError(t, err)

		// Create interim
		_, err = ms.CreateInterim(ctx, &types.MsgCreateInterim{
			Creator:       creatorStr,
			InterimType:   types.InterimType_INTERIM_TYPE_CONTRIBUTION_REVIEW,
			ReferenceId:   initID,
			ReferenceType: "initiative",
			Complexity:    types.InterimComplexity_INTERIM_COMPLEXITY_STANDARD,
			Deadline:      12345,
		})
		require.NoError(t, err)

		// Verify interim exists
		initiative, err := k.GetInitiative(ctx, initID)
		require.NoError(t, err)
		require.Equal(t, types.InitiativeStatus_INITIATIVE_STATUS_OPEN, initiative.Status)
	})
}

package keeper_test

import (
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

func TestMsgServerCompleteInitiative(t *testing.T) {
	t.Run("invalid creator address", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		_, err := ms.CompleteInitiative(f.ctx, &types.MsgCompleteInitiative{
			Creator:      "invalid-address",
			InitiativeId: 1,
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

		_, err = ms.CompleteInitiative(f.ctx, &types.MsgCompleteInitiative{
			Creator:      creatorStr,
			InitiativeId: 99999,
		})

		require.Error(t, err)
		require.Contains(t, err.Error(), "not found")
	})

	t.Run("successful completion", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)
		k := f.keeper
		ctx := f.ctx

		// Setup: create project, member, and initiative
		creator := sdk.AccAddress([]byte("creator"))
		k.Member.Set(ctx, creator.String(), types.Member{
			Address:          creator.String(),
			DreamBalance:     keeper.PtrInt(math.NewInt(10000)),
			StakedDream:      keeper.PtrInt(math.ZeroInt()),
			LifetimeEarned:   keeper.PtrInt(math.ZeroInt()),
			LifetimeBurned:   keeper.PtrInt(math.ZeroInt()),
			ReputationScores: map[string]string{"tag": "100.0"},
		})
		projectID, _ := k.CreateProject(ctx, creator, "Proj", "Desc", []string{"tag"}, types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "technical", math.NewInt(10000), math.NewInt(1000), false)
		k.ApproveProject(ctx, projectID, sdk.AccAddress([]byte("approver")), math.NewInt(10000), math.NewInt(1000))

		budget := math.NewInt(100)
		initID, _ := k.CreateInitiative(ctx, creator, projectID, "Task", "D", []string{"tag"}, types.InitiativeTier_INITIATIVE_TIER_STANDARD, types.InitiativeCategory_INITIATIVE_CATEGORY_FEATURE, budget)

		// Create assignee member
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

		err = k.AssignInitiativeToMember(ctx, initID, assignee)
		require.NoError(t, err)
		err = k.SubmitInitiativeWork(ctx, initID, assignee, "uri")
		require.NoError(t, err)

		// Modify initiative to have 0 required conviction for testing completion logic
		// This bypasses the need for waiting for time decay or creating massive stakes
		initiative, err := k.GetInitiative(ctx, initID)
		require.NoError(t, err)
		initiative.RequiredConviction = keeper.PtrDec(math.LegacyZeroDec())
		err = k.Initiative.Set(ctx, initID, initiative)
		require.NoError(t, err)

		// Manual completion is only accepted once the initiative has reached
		// IN_REVIEW and its challenge window has closed, which is what the
		// EndBlocker would have done by this point in a real chain.
		advanceToCompletable(t, k, ctx, initID)

		creatorStr, err := f.addressCodec.BytesToString(creator)
		require.NoError(t, err)

		_, err = ms.CompleteInitiative(ctx, &types.MsgCompleteInitiative{
			Creator:      creatorStr,
			InitiativeId: initID,
		})
		require.NoError(t, err)
	})
}

// TestCompleteInitiativeRespectsChallengeWindow pins the guarantee the challenge
// period is supposed to make: an assignee cannot reach payout before the window
// in which their work can be contested has actually run. Both halves of the old
// bypass are covered — completing straight out of SUBMITTED, and completing from
// IN_REVIEW while the window is still open.
func TestCompleteInitiativeRespectsChallengeWindow(t *testing.T) {
	// setup builds a fully-funded initiative whose conviction gates are met and
	// whose work has been submitted, returning the assignee's bech32 address.
	setup := func(t *testing.T, f *fixture) (uint64, string) {
		t.Helper()
		k, ctx := f.keeper, f.ctx

		creator := sdk.AccAddress([]byte("cw-creator________"))
		k.Member.Set(ctx, creator.String(), types.Member{
			Address:          creator.String(),
			DreamBalance:     keeper.PtrInt(math.NewInt(100000)),
			StakedDream:      keeper.PtrInt(math.ZeroInt()),
			LifetimeEarned:   keeper.PtrInt(math.ZeroInt()),
			LifetimeBurned:   keeper.PtrInt(math.ZeroInt()),
			ReputationScores: map[string]string{"tag": "100.0"},
		})
		projectID, err := k.CreateProject(ctx, creator, "P", "D", []string{"tag"},
			types.ProjectCategory_PROJECT_CATEGORY_INFRASTRUCTURE, "technical",
			math.NewInt(10000), math.NewInt(0), false)
		require.NoError(t, err)
		require.NoError(t, k.ApproveProject(ctx, projectID, creator, math.NewInt(10000), math.NewInt(0)))

		initID, err := k.CreateInitiative(ctx, creator, projectID, "T", "D", []string{"tag"},
			types.InitiativeTier_INITIATIVE_TIER_STANDARD,
			types.InitiativeCategory_INITIATIVE_CATEGORY_FEATURE, math.NewInt(100))
		require.NoError(t, err)

		assignee := sdk.AccAddress([]byte("cw-assignee_______"))
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

		require.NoError(t, k.AssignInitiativeToMember(ctx, initID, assignee))
		require.NoError(t, k.SubmitInitiativeWork(ctx, initID, assignee, "ipfs://work"))

		// Both conviction gates satisfied, so status is the only thing left
		// standing between the assignee and the payout.
		ini, err := k.GetInitiative(ctx, initID)
		require.NoError(t, err)
		ini.RequiredConviction = keeper.PtrDec(math.LegacyZeroDec())
		ini.CurrentConviction = keeper.PtrDec(math.LegacyNewDec(1000))
		ini.ExternalConviction = keeper.PtrDec(math.LegacyNewDec(1000))
		require.NoError(t, k.UpdateInitiative(ctx, ini))

		return initID, assigneeStr
	}

	assertUnpaid := func(t *testing.T, f *fixture, assigneeStr string) {
		t.Helper()
		member, err := f.keeper.GetMember(f.ctx, sdk.MustAccAddressFromBech32(assigneeStr))
		require.NoError(t, err)
		require.Equal(t, math.ZeroInt().String(), keeper.DerefInt(member.DreamBalance).String(),
			"no DREAM should have been minted")
	}

	t.Run("assignee cannot complete straight out of SUBMITTED", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)
		initID, assigneeStr := setup(t, f)

		ini, err := f.keeper.GetInitiative(f.ctx, initID)
		require.NoError(t, err)
		require.Equal(t, types.InitiativeStatus_INITIATIVE_STATUS_SUBMITTED, ini.Status)

		_, err = ms.CompleteInitiative(f.ctx, &types.MsgCompleteInitiative{
			Creator:      assigneeStr,
			InitiativeId: initID,
		})
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrInvalidInitiativeStatus)
		assertUnpaid(t, f, assigneeStr)
	})

	t.Run("assignee cannot complete while the challenge window is open", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)
		initID, assigneeStr := setup(t, f)

		// The EndBlocker's own transition: conviction met, so the initiative
		// moves into review with a challenge window still ahead of it.
		require.NoError(t, f.keeper.TransitionToChallengePeriod(f.ctx, initID))
		ini, err := f.keeper.GetInitiative(f.ctx, initID)
		require.NoError(t, err)
		require.Equal(t, types.InitiativeStatus_INITIATIVE_STATUS_IN_REVIEW, ini.Status)
		require.Greater(t, ini.ChallengePeriodEnd, f.ctx.BlockHeight(),
			"challenge window should still be open")

		_, err = ms.CompleteInitiative(f.ctx, &types.MsgCompleteInitiative{
			Creator:      assigneeStr,
			InitiativeId: initID,
		})
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrChallengePeriodActive)
		assertUnpaid(t, f, assigneeStr)

		// Once the window closes the same call goes through, so the guard
		// delays the payout rather than blocking it.
		closed := f.ctx.WithBlockHeight(ini.ChallengePeriodEnd)
		_, err = ms.CompleteInitiative(closed, &types.MsgCompleteInitiative{
			Creator:      assigneeStr,
			InitiativeId: initID,
		})
		require.NoError(t, err)

		member, err := f.keeper.GetMember(closed, sdk.MustAccAddressFromBech32(assigneeStr))
		require.NoError(t, err)
		require.True(t, keeper.DerefInt(member.DreamBalance).IsPositive(),
			"assignee should be paid once the window has closed")
	})
}

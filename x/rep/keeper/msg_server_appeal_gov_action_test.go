package keeper_test

import (
	"context"
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

// setActiveMember seeds an active member with minimal fields.
func setActiveMember(t *testing.T, k keeper.Keeper, ctx sdk.Context, addr sdk.AccAddress) {
	t.Helper()
	err := k.Member.Set(ctx, addr.String(), types.Member{
		Address:        addr.String(),
		Status:         types.MemberStatus_MEMBER_STATUS_ACTIVE,
		DreamBalance:   keeper.PtrInt(math.NewInt(1000)),
		StakedDream:    keeper.PtrInt(math.ZeroInt()),
		LifetimeEarned: keeper.PtrInt(math.NewInt(1000)),
		LifetimeBurned: keeper.PtrInt(math.ZeroInt()),
		TrustLevel:     types.TrustLevel_TRUST_LEVEL_ESTABLISHED,
	})
	require.NoError(t, err)
}

func TestMsgServerAppealGovAction(t *testing.T) {
	t.Run("invalid creator address", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		_, err := ms.AppealGovAction(f.ctx, &types.MsgAppealGovAction{
			Creator:      "invalid",
			ActionType:   uint64(types.GovActionType_GOV_ACTION_TYPE_WARNING),
			ActionTarget: "some-target",
			AppealReason: "I disagree",
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid creator address")
	})

	t.Run("non-member cannot appeal", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		nonMember := sdk.AccAddress([]byte("not_a_member____"))
		nonMemberStr, err := f.addressCodec.BytesToString(nonMember)
		require.NoError(t, err)

		_, err = ms.AppealGovAction(f.ctx, &types.MsgAppealGovAction{
			Creator:      nonMemberStr,
			ActionType:   uint64(types.GovActionType_GOV_ACTION_TYPE_WARNING),
			ActionTarget: "something",
			AppealReason: "I want to appeal",
		})
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrNotMember)
	})

	t.Run("invalid action type", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		appellant := sdk.AccAddress([]byte("appellant_bad___"))
		setActiveMember(t, f.keeper, f.ctx, appellant)
		appellantStr, err := f.addressCodec.BytesToString(appellant)
		require.NoError(t, err)

		_, err = ms.AppealGovAction(f.ctx, &types.MsgAppealGovAction{
			Creator:      appellantStr,
			ActionType:   uint64(types.GovActionType_GOV_ACTION_TYPE_UNSPECIFIED),
			ActionTarget: "target-x",
			AppealReason: "I disagree",
		})
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrInvalidReasonCode)
	})

	t.Run("appeal already filed", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		appellant := sdk.AccAddress([]byte("appellant_dup___"))
		setActiveMember(t, f.keeper, f.ctx, appellant)
		appellantStr, err := f.addressCodec.BytesToString(appellant)
		require.NoError(t, err)

		// First appeal succeeds.
		_, err = ms.AppealGovAction(f.ctx, &types.MsgAppealGovAction{
			Creator:      appellantStr,
			ActionType:   uint64(types.GovActionType_GOV_ACTION_TYPE_WARNING),
			ActionTarget: "target-dup",
			AppealReason: "first try",
		})
		require.NoError(t, err)

		// Second appeal on the same action is rejected.
		_, err = ms.AppealGovAction(f.ctx, &types.MsgAppealGovAction{
			Creator:      appellantStr,
			ActionType:   uint64(types.GovActionType_GOV_ACTION_TYPE_WARNING),
			ActionTarget: "target-dup",
			AppealReason: "second try",
		})
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrAppealAlreadyFiled)
	})

	t.Run("successful appeal creates record and initiative", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)

		appellant := sdk.AccAddress([]byte("appellant_happy_"))
		setActiveMember(t, f.keeper, f.ctx, appellant)
		appellantStr, err := f.addressCodec.BytesToString(appellant)
		require.NoError(t, err)

		_, err = ms.AppealGovAction(f.ctx, &types.MsgAppealGovAction{
			Creator:      appellantStr,
			ActionType:   uint64(types.GovActionType_GOV_ACTION_TYPE_WARNING),
			ActionTarget: "target-happy",
			AppealReason: "unjustified warning",
		})
		require.NoError(t, err)

		// Appeal record exists with expected content.
		foundAppeal := false
		require.NoError(t, f.keeper.GovActionAppeal.Walk(f.ctx, nil, func(_ uint64, a types.GovActionAppeal) (bool, error) {
			if a.Appellant == appellantStr && a.ActionTarget == "target-happy" {
				foundAppeal = true
				require.Equal(t, types.GovActionType_GOV_ACTION_TYPE_WARNING, a.ActionType)
				require.Equal(t, types.GovAppealStatus_GOV_APPEAL_STATUS_PENDING, a.Status)
				require.NotZero(t, a.InitiativeId)
				require.Equal(t, "unjustified warning", a.AppealReason)
				// Stage C: bond and deadline now populated on creation.
				require.Equal(t, math.NewInt(types.DefaultAppealBondAmount).String(), a.AppealBond)
				require.Equal(t, a.CreatedAt+types.DefaultAppealDeadline, a.Deadline)
			}
			return false, nil
		}))
		require.True(t, foundAppeal, "expected a GovActionAppeal record")
	})

	t.Run("bond transfer failure surfaces as error", func(t *testing.T) {
		f := initFixture(t)
		// Force the bank send-to-escrow to fail: simulates insufficient funds.
		f.bankKeeper.SendCoinsFn = func(_ context.Context, _ sdk.AccAddress, _ sdk.AccAddress, _ sdk.Coins) error {
			return types.ErrInsufficientBalance
		}

		ms := keeper.NewMsgServerImpl(f.keeper)
		appellant := sdk.AccAddress([]byte("appellant_broke_"))
		setActiveMember(t, f.keeper, f.ctx, appellant)
		appellantStr, err := f.addressCodec.BytesToString(appellant)
		require.NoError(t, err)

		_, err = ms.AppealGovAction(f.ctx, &types.MsgAppealGovAction{
			Creator:      appellantStr,
			ActionType:   uint64(types.GovActionType_GOV_ACTION_TYPE_WARNING),
			ActionTarget: "target-broke",
			AppealReason: "please",
		})
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrInsufficientBalance)

		// No appeal record should have been stored.
		iter, err := f.keeper.GovActionAppeal.Iterate(f.ctx, nil)
		require.NoError(t, err)
		defer iter.Close()
		require.False(t, iter.Valid(), "no appeal record should exist after bond failure")
	})
}

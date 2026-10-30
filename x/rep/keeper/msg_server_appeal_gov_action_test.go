package keeper_test

import (
	"context"
	"testing"
	"time"

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
		appellant, appellantStr := seedWarnedMember(t, f, "appellant_dup___")

		// First appeal succeeds.
		_, err := ms.AppealGovAction(f.ctx, &types.MsgAppealGovAction{
			Creator:      appellantStr,
			ActionType:   uint64(types.GovActionType_GOV_ACTION_TYPE_WARNING),
			ActionTarget: appellantStr,
			AppealReason: "first try",
		})
		require.NoError(t, err)
		_ = appellant

		// Second appeal on the same resolution is rejected.
		_, err = ms.AppealGovAction(f.ctx, &types.MsgAppealGovAction{
			Creator:      appellantStr,
			ActionType:   uint64(types.GovActionType_GOV_ACTION_TYPE_WARNING),
			ActionTarget: appellantStr,
			AppealReason: "second try",
		})
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrAppealAlreadyFiled)
	})

	t.Run("successful appeal creates record and initiative", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)
		_, appellantStr := seedWarnedMember(t, f, "appellant_happy_")

		_, err := ms.AppealGovAction(f.ctx, &types.MsgAppealGovAction{
			Creator:      appellantStr,
			ActionType:   uint64(types.GovActionType_GOV_ACTION_TYPE_WARNING),
			ActionTarget: appellantStr,
			AppealReason: "unjustified warning",
		})
		require.NoError(t, err)

		// Appeal record exists with expected content.
		foundAppeal := false
		require.NoError(t, f.keeper.GovActionAppeal.Walk(f.ctx, nil, func(_ uint64, a types.GovActionAppeal) (bool, error) {
			if a.Appellant == appellantStr && a.ActionTarget == appellantStr {
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

	// FRM-02 / REP-R-14: only the affected member, only an action actually
	// applied, only inside the filing window.
	t.Run("third party cannot appeal another member's warning", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)
		_, warnedStr := seedWarnedMember(t, f, "appellant_warned")
		other := sdk.AccAddress([]byte("appellant_other_"))
		setActiveMember(t, f.keeper, f.ctx, other)

		_, err := ms.AppealGovAction(f.ctx, &types.MsgAppealGovAction{
			Creator: other.String(), ActionType: uint64(types.GovActionType_GOV_ACTION_TYPE_WARNING),
			ActionTarget: warnedStr, AppealReason: "pre-empting",
		})
		require.ErrorIs(t, err, types.ErrAppealNotAllowed)
	})

	t.Run("no applied action, wrong action, closed window, unappealable type", func(t *testing.T) {
		f := initFixture(t)
		ms := keeper.NewMsgServerImpl(f.keeper)
		plain := sdk.AccAddress([]byte("appellant_plain_"))
		setActiveMember(t, f.keeper, f.ctx, plain)
		_, err := ms.AppealGovAction(f.ctx, &types.MsgAppealGovAction{
			Creator: plain.String(), ActionType: uint64(types.GovActionType_GOV_ACTION_TYPE_WARNING),
			ActionTarget: plain.String(), AppealReason: "nothing happened",
		})
		require.ErrorIs(t, err, types.ErrAppealNotAllowed)

		_, warnedStr := seedWarnedMember(t, f, "appellant_window")
		_, err = ms.AppealGovAction(f.ctx, &types.MsgAppealGovAction{
			Creator: warnedStr, ActionType: uint64(types.GovActionType_GOV_ACTION_TYPE_ZEROING),
			ActionTarget: warnedStr, AppealReason: "wrong action",
		})
		require.ErrorIs(t, err, types.ErrAppealNotAllowed)

		late := f.ctx.WithBlockTime(f.ctx.BlockTime().Add(time.Duration(types.DefaultAppealDeadline+1) * time.Second))
		_, err = ms.AppealGovAction(late, &types.MsgAppealGovAction{
			Creator: warnedStr, ActionType: uint64(types.GovActionType_GOV_ACTION_TYPE_WARNING),
			ActionTarget: warnedStr, AppealReason: "too late",
		})
		require.ErrorIs(t, err, types.ErrAppealNotAllowed)

		for _, at := range []types.GovActionType{types.GovActionType_GOV_ACTION_TYPE_TAG_REMOVAL, types.GovActionType_GOV_ACTION_TYPE_FORUM_PAUSE, types.GovActionType_GOV_ACTION_TYPE_REVIEW_REJECTION} {
			_, err = ms.AppealGovAction(f.ctx, &types.MsgAppealGovAction{
				Creator: warnedStr, ActionType: uint64(at), ActionTarget: warnedStr, AppealReason: "x",
			})
			require.ErrorIs(t, err, types.ErrAppealNotAllowed, at.String())
		}
	})

	t.Run("bond transfer failure surfaces as error", func(t *testing.T) {
		f := initFixture(t)
		// Force the bank send-to-escrow to fail: simulates insufficient funds.
		f.bankKeeper.SendCoinsFn = func(_ context.Context, _ sdk.AccAddress, _ sdk.AccAddress, _ sdk.Coins) error {
			return types.ErrInsufficientBalance
		}

		ms := keeper.NewMsgServerImpl(f.keeper)
		_, appellantStr := seedWarnedMember(t, f, "appellant_broke_")

		_, err := ms.AppealGovAction(f.ctx, &types.MsgAppealGovAction{
			Creator:      appellantStr,
			ActionType:   uint64(types.GovActionType_GOV_ACTION_TYPE_WARNING),
			ActionTarget: appellantStr,
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

// seedWarnedMember seeds an active member whose member report was resolved
// with a WARNING just now, which opens their MsgAppealGovAction window.
func seedWarnedMember(t *testing.T, f *fixture, name string) (sdk.AccAddress, string) {
	t.Helper()
	addr := sdk.AccAddress([]byte(name))
	setActiveMember(t, f.keeper, f.ctx, addr)
	require.NoError(t, f.keeper.MemberReport.Set(f.ctx, addr.String(), types.MemberReport{
		Member:         addr.String(),
		Status:         types.MemberReportStatus_MEMBER_REPORT_STATUS_RESOLVED,
		ResolvedAction: types.GovActionType_GOV_ACTION_TYPE_WARNING,
		ResolvedAt:     f.ctx.BlockTime().Unix(),
	}))
	require.NoError(t, f.keeper.MemberActionRestorations.Set(f.ctx, addr.String(), types.MemberActionRestoration{
		Member:     addr.String(),
		Action:     types.GovActionType_GOV_ACTION_TYPE_WARNING,
		ResolvedAt: f.ctx.BlockTime().Unix(),
	}))
	return addr, addr.String()
}

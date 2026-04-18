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

// resolveFixture is a helper bundling the common scaffolding required by the
// resolve-gov-action-appeal tests: fixture + msg server + wired forum mock +
// an active appellant with a pending appeal whose bond has been transferred
// into the rep module account via the real AppealGovAction flow.
type resolveFixture struct {
	f             *fixture
	ms            types.MsgServer
	fk            *mockForumKeeper
	appellant     sdk.AccAddress
	appellantStr  string
	actionType    types.GovActionType
	actionTarget  string
	sentinel      string
	refundedCoins sdk.Coins
	burnedCoins   sdk.Coins
}

func setupResolveFixture(t *testing.T, opts ...FixtureOption) *resolveFixture {
	t.Helper()

	f := initFixture(t, opts...)
	// initFixture wires IsCommitteeMember from authorizationPolicy, but leaves
	// IsCouncilAuthorized defaulting to false. Mirror the policy here so the
	// rep helper's isCouncilAuthorized shortcut works too.
	if f.commonsKeeper.IsCouncilAuthorizedFn == nil {
		f.commonsKeeper.IsCouncilAuthorizedFn = func(_ context.Context, _ string, _ string, _ string) bool { return true }
	}
	ms := keeper.NewMsgServerImpl(f.keeper)

	fk := &mockForumKeeper{
		authors:         make(map[uint64]string),
		tags:            make(map[uint64][]string),
		actionSentinels: make(map[string]string),
	}
	f.keeper.SetForumKeeper(fk)

	rf := &resolveFixture{
		f:            f,
		ms:           ms,
		fk:           fk,
		actionType:   types.GovActionType_GOV_ACTION_TYPE_WARNING,
		actionTarget: "target-xyz",
		sentinel:     sdk.AccAddress([]byte("sentinel_happy__")).String(),
	}

	// Observe bank transfers / burns.
	f.bankKeeper.SendCoinsFromModuleToAccountFn = func(_ context.Context, _ string, _ sdk.AccAddress, amt sdk.Coins) error {
		rf.refundedCoins = rf.refundedCoins.Add(amt...)
		return nil
	}
	f.bankKeeper.BurnCoinsFn = func(_ context.Context, _ string, amt sdk.Coins) error {
		rf.burnedCoins = rf.burnedCoins.Add(amt...)
		return nil
	}

	// Register the sentinel mapping on the forum mock.
	fk.actionSentinels[mockForumKey(rf.actionType, rf.actionTarget)] = rf.sentinel

	// Seed appellant as active member and file the appeal.
	appellant := sdk.AccAddress([]byte("appellant_resolv"))
	setActiveMember(t, f.keeper, f.ctx, appellant)
	appellantStr, err := f.addressCodec.BytesToString(appellant)
	require.NoError(t, err)
	rf.appellant = appellant
	rf.appellantStr = appellantStr

	_, err = ms.AppealGovAction(f.ctx, &types.MsgAppealGovAction{
		Creator:      appellantStr,
		ActionType:   uint64(rf.actionType),
		ActionTarget: rf.actionTarget,
		AppealReason: "testing",
	})
	require.NoError(t, err)

	return rf
}

// seedSlashableSentinel creates rep sentinel + member records so SlashBond
// can succeed against the sentinel address.
func seedSlashableSentinel(t *testing.T, f *fixture, sentinel string, bond math.Int) {
	t.Helper()
	saBytes, err := f.addressCodec.StringToBytes(sentinel)
	require.NoError(t, err)
	saAddr := sdk.AccAddress(saBytes)

	err = f.keeper.Member.Set(f.ctx, sentinel, types.Member{
		Address:        sentinel,
		Status:         types.MemberStatus_MEMBER_STATUS_ACTIVE,
		DreamBalance:   keeper.PtrInt(bond),
		StakedDream:    keeper.PtrInt(bond),
		LifetimeEarned: keeper.PtrInt(bond),
		LifetimeBurned: keeper.PtrInt(math.ZeroInt()),
		TrustLevel:     types.TrustLevel_TRUST_LEVEL_ESTABLISHED,
	})
	require.NoError(t, err)
	_ = saAddr

	err = f.keeper.SentinelActivity.Set(f.ctx, sentinel, types.SentinelActivity{
		Address:            sentinel,
		CurrentBond:        bond.String(),
		TotalCommittedBond: "0",
		BondStatus:         types.SentinelBondStatus_SENTINEL_BOND_STATUS_NORMAL,
	})
	require.NoError(t, err)
}

func findAppeal(t *testing.T, f *fixture, appellant string) (uint64, types.GovActionAppeal) {
	t.Helper()
	iter, err := f.keeper.GovActionAppeal.Iterate(f.ctx, nil)
	require.NoError(t, err)
	defer iter.Close()
	for ; iter.Valid(); iter.Next() {
		kv, err := iter.KeyValue()
		require.NoError(t, err)
		if kv.Value.Appellant == appellant {
			return kv.Key, kv.Value
		}
	}
	t.Fatalf("no appeal found for appellant %s", appellant)
	return 0, types.GovActionAppeal{}
}

func TestMsgServerResolveGovActionAppeal(t *testing.T) {
	t.Run("upheld burns half and retains half; forum hook invoked", func(t *testing.T) {
		rf := setupResolveFixture(t)
		appealID, _ := findAppeal(t, rf.f, rf.appellantStr)

		resolver := sdk.AccAddress([]byte("council_resolver"))
		resolverStr, err := rf.f.addressCodec.BytesToString(resolver)
		require.NoError(t, err)

		_, err = rf.ms.ResolveGovActionAppeal(rf.f.ctx, &types.MsgResolveGovActionAppeal{
			Resolver: resolverStr,
			AppealId: appealID,
			Verdict:  types.GovAppealStatus_GOV_APPEAL_STATUS_UPHELD,
			Reason:   "warning justified",
		})
		require.NoError(t, err)

		_, updated := findAppeal(t, rf.f, rf.appellantStr)
		require.Equal(t, types.GovAppealStatus_GOV_APPEAL_STATUS_UPHELD, updated.Status)

		// Half of the bond burned; the other half stays on the rep module
		// account (no refund fired).
		expectedHalf := math.NewInt(types.DefaultAppealBondAmount).QuoRaw(2)
		require.True(t,
			rf.burnedCoins.AmountOf(types.RewardDenom).Equal(expectedHalf),
			"expected %s burned, got %s", expectedHalf, rf.burnedCoins.AmountOf(types.RewardDenom))
		require.True(t, rf.refundedCoins.IsZero(), "no refund should occur on UPHELD")

		// Upheld forum hook invoked exactly once.
		require.Len(t, rf.fk.upheldCalls, 1)
		require.Empty(t, rf.fk.overturnedCalls)
	})

	t.Run("overturned refunds full bond and slashes sentinel", func(t *testing.T) {
		rf := setupResolveFixture(t)
		appealID, _ := findAppeal(t, rf.f, rf.appellantStr)

		bond := math.NewInt(1_000_000_000) // ample
		seedSlashableSentinel(t, rf.f, rf.sentinel, bond)

		resolver := sdk.AccAddress([]byte("council_resolver"))
		resolverStr, err := rf.f.addressCodec.BytesToString(resolver)
		require.NoError(t, err)

		_, err = rf.ms.ResolveGovActionAppeal(rf.f.ctx, &types.MsgResolveGovActionAppeal{
			Resolver: resolverStr,
			AppealId: appealID,
			Verdict:  types.GovAppealStatus_GOV_APPEAL_STATUS_OVERTURNED,
			Reason:   "sentinel wrong",
		})
		require.NoError(t, err)

		_, updated := findAppeal(t, rf.f, rf.appellantStr)
		require.Equal(t, types.GovAppealStatus_GOV_APPEAL_STATUS_OVERTURNED, updated.Status)

		// Full bond refunded to appellant; no burn.
		expectedBond := math.NewInt(types.DefaultAppealBondAmount)
		require.True(t,
			rf.refundedCoins.AmountOf(types.RewardDenom).Equal(expectedBond),
			"expected %s refunded, got %s", expectedBond, rf.refundedCoins.AmountOf(types.RewardDenom))
		require.True(t, rf.burnedCoins.AmountOf(types.RewardDenom).IsZero())

		// Sentinel's bond was reduced by DefaultSentinelOverturnSlash.
		sa, err := rf.f.keeper.SentinelActivity.Get(rf.f.ctx, rf.sentinel)
		require.NoError(t, err)
		expectedRemaining := bond.SubRaw(types.DefaultSentinelOverturnSlash)
		require.Equal(t, expectedRemaining.String(), sa.CurrentBond)

		// Overturned forum hook invoked.
		require.Len(t, rf.fk.overturnedCalls, 1)
		require.Empty(t, rf.fk.upheldCalls)
	})

	t.Run("rejects non-council resolver", func(t *testing.T) {
		rf := setupResolveFixture(t, WithAuthorizationPolicy(NeverAuthorized))
		// Override IsCouncilAuthorized too — setupResolveFixture defaults it
		// to permissive, but this test exercises the denied path.
		rf.f.commonsKeeper.IsCouncilAuthorizedFn = func(_ context.Context, _ string, _ string, _ string) bool { return false }
		appealID, _ := findAppeal(t, rf.f, rf.appellantStr)

		resolver := sdk.AccAddress([]byte("random_outsider_"))
		resolverStr, err := rf.f.addressCodec.BytesToString(resolver)
		require.NoError(t, err)

		_, err = rf.ms.ResolveGovActionAppeal(rf.f.ctx, &types.MsgResolveGovActionAppeal{
			Resolver: resolverStr,
			AppealId: appealID,
			Verdict:  types.GovAppealStatus_GOV_APPEAL_STATUS_UPHELD,
			Reason:   "no",
		})
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrNotGovAuthority)
	})

	t.Run("rejects unknown appeal id", func(t *testing.T) {
		f := initFixture(t)
		f.commonsKeeper.IsCouncilAuthorizedFn = func(_ context.Context, _ string, _ string, _ string) bool { return true }
		ms := keeper.NewMsgServerImpl(f.keeper)

		resolver := sdk.AccAddress([]byte("council_resolver"))
		resolverStr, err := f.addressCodec.BytesToString(resolver)
		require.NoError(t, err)

		_, err = ms.ResolveGovActionAppeal(f.ctx, &types.MsgResolveGovActionAppeal{
			Resolver: resolverStr,
			AppealId: 999,
			Verdict:  types.GovAppealStatus_GOV_APPEAL_STATUS_UPHELD,
			Reason:   "ghost",
		})
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrAppealNotFound)
	})

	t.Run("rejects already-resolved appeal", func(t *testing.T) {
		rf := setupResolveFixture(t)
		appealID, appeal := findAppeal(t, rf.f, rf.appellantStr)
		appeal.Status = types.GovAppealStatus_GOV_APPEAL_STATUS_UPHELD
		require.NoError(t, rf.f.keeper.GovActionAppeal.Set(rf.f.ctx, appealID, appeal))

		resolver := sdk.AccAddress([]byte("council_resolver"))
		resolverStr, err := rf.f.addressCodec.BytesToString(resolver)
		require.NoError(t, err)

		_, err = rf.ms.ResolveGovActionAppeal(rf.f.ctx, &types.MsgResolveGovActionAppeal{
			Resolver: resolverStr,
			AppealId: appealID,
			Verdict:  types.GovAppealStatus_GOV_APPEAL_STATUS_OVERTURNED,
			Reason:   "late",
		})
		require.Error(t, err)
		require.ErrorIs(t, err, types.ErrAppealNotPending)
	})

	t.Run("rejects UNSPECIFIED and TIMEOUT verdicts", func(t *testing.T) {
		for _, v := range []types.GovAppealStatus{
			types.GovAppealStatus_GOV_APPEAL_STATUS_UNSPECIFIED,
			types.GovAppealStatus_GOV_APPEAL_STATUS_TIMEOUT,
			types.GovAppealStatus_GOV_APPEAL_STATUS_PENDING,
		} {
			rf := setupResolveFixture(t)
			appealID, _ := findAppeal(t, rf.f, rf.appellantStr)

			resolver := sdk.AccAddress([]byte("council_resolver"))
			resolverStr, err := rf.f.addressCodec.BytesToString(resolver)
			require.NoError(t, err)

			_, err = rf.ms.ResolveGovActionAppeal(rf.f.ctx, &types.MsgResolveGovActionAppeal{
				Resolver: resolverStr,
				AppealId: appealID,
				Verdict:  v,
				Reason:   "bad verdict",
			})
			require.Error(t, err, "verdict %s must be rejected", v.String())
			require.ErrorIs(t, err, types.ErrInvalidAppealVerdict, "verdict %s", v.String())
		}
	})
}

func TestTimeoutExpiredAppeals(t *testing.T) {
	f := initFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	fk := &mockForumKeeper{
		authors:         make(map[uint64]string),
		tags:            make(map[uint64][]string),
		actionSentinels: make(map[string]string),
	}
	f.keeper.SetForumKeeper(fk)

	var refunded, burned sdk.Coins
	f.bankKeeper.SendCoinsFromModuleToAccountFn = func(_ context.Context, _ string, _ sdk.AccAddress, amt sdk.Coins) error {
		refunded = refunded.Add(amt...)
		return nil
	}
	f.bankKeeper.BurnCoinsFn = func(_ context.Context, _ string, amt sdk.Coins) error {
		burned = burned.Add(amt...)
		return nil
	}

	appellant := sdk.AccAddress([]byte("appellant_tmout_"))
	setActiveMember(t, f.keeper, f.ctx, appellant)
	appellantStr, err := f.addressCodec.BytesToString(appellant)
	require.NoError(t, err)

	_, err = ms.AppealGovAction(f.ctx, &types.MsgAppealGovAction{
		Creator:      appellantStr,
		ActionType:   uint64(types.GovActionType_GOV_ACTION_TYPE_WARNING),
		ActionTarget: "target-timeout",
		AppealReason: "timeout path",
	})
	require.NoError(t, err)

	// Advance block time past the deadline.
	appealID, appeal := findAppeal(t, f, appellantStr)
	require.Equal(t, types.GovAppealStatus_GOV_APPEAL_STATUS_PENDING, appeal.Status)
	futureCtx := f.ctx.WithBlockTime(f.ctx.BlockTime().Add(
		time.Duration(types.DefaultAppealDeadline+10) * time.Second,
	))

	require.NoError(t, f.keeper.TimeoutExpiredAppeals(futureCtx))

	_, updated := findAppeal(t, f, appellantStr)
	require.Equal(t, types.GovAppealStatus_GOV_APPEAL_STATUS_TIMEOUT, updated.Status)

	bond := math.NewInt(types.DefaultAppealBondAmount)
	half := bond.QuoRaw(2)
	refund := bond.Sub(half)
	require.Equal(t, refund.String(), refunded.AmountOf(types.RewardDenom).String())
	require.Equal(t, half.String(), burned.AmountOf(types.RewardDenom).String())

	// Running again is a no-op — no more pending appeals.
	refunded = sdk.NewCoins()
	burned = sdk.NewCoins()
	require.NoError(t, f.keeper.TimeoutExpiredAppeals(futureCtx))
	require.True(t, refunded.IsZero())
	require.True(t, burned.IsZero())
	_ = appealID
}

package keeper_test

import (
	"fmt"
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	reptypes "sparkdream/x/rep/types"
	"sparkdream/x/season/keeper"
	"sparkdream/x/season/types"
)

// seedModeration writes an active moderation of target with the reporter's
// stake and, when appellant is set, an appeal and its stake.
func seedModeration(t *testing.T, f *beginBlockFixture, ctx sdk.Context, target, reporter, appellant string, at int64) string {
	t.Helper()
	reportID := fmt.Sprintf("dn:%s:%d", target, at)
	m := types.DisplayNameModeration{Member: target, RejectedName: "Rogue", ModeratedAt: at, Active: true}
	if appellant != "" {
		m.AppealChallengeId = "dna:" + target
		m.AppealedAt = at + 1
		require.NoError(t, f.keeper.DisplayNameAppealStake.Set(ctx, m.AppealChallengeId, types.DisplayNameAppealStake{
			ChallengeId: m.AppealChallengeId, Appellant: appellant, Amount: math.NewInt(50),
		}))
	}
	require.NoError(t, f.keeper.SetDisplayNameModeration(ctx, target, m))
	require.NoError(t, f.keeper.DisplayNameReportStake.Set(ctx, reportID, types.DisplayNameReportStake{
		ChallengeId: reportID, Reporter: reporter, Amount: math.NewInt(50),
	}))
	return reportID
}

// SEA-101: a zeroed reporter's or appellant's display-name stake is dropped
// by the zeroing hook, and every resolution path then closes the case
// instead of failing on the missing lock for good.
func TestSEA101_ZeroedStakerNoLongerWedgesModeration(t *testing.T) {
	f := initBeginBlockFixture(t)
	target := TestAddrMember1.String()
	reporter := TestAddrMember2.String()
	params, err := f.keeper.Params.Get(f.ctx)
	require.NoError(t, err)
	late := f.ctx.WithBlockHeight(100 + int64(params.DisplayNameAppealPeriodBlocks) + 1)
	hook := keeper.NewZeroingHook(&f.keeper)
	// x/rep's answer for a zeroed member's lock.
	gone := fmt.Errorf("unlock: %w", reptypes.ErrInsufficientStake)

	t.Run("unappealed, hook dropped the stake", func(t *testing.T) {
		ctx, _ := f.ctx.CacheContext()
		reportID := seedModeration(t, f, ctx, target, reporter, "", 100)
		require.NoError(t, hook.AfterMemberZeroed(ctx, reporter, []string{reporter}))
		has, err := f.keeper.DisplayNameReportStake.Has(ctx, reportID)
		require.NoError(t, err)
		require.False(t, has)

		f.repKeeper.UnlockErr = gone // any unlock now would fail
		defer func() { f.repKeeper.UnlockErr = nil }()
		lctx := late.WithMultiStore(ctx.MultiStore())
		require.NoError(t, f.keeper.ResolveUnappealedModerationInternal(lctx, target))
		m, err := f.keeper.DisplayNameModeration.Get(lctx, target)
		require.NoError(t, err)
		require.False(t, m.Active)
		require.Equal(t, []string{"rogue"}, m.BlockedNames)
	})

	t.Run("unappealed, record missed by the hook", func(t *testing.T) {
		ctx, _ := f.ctx.CacheContext()
		reportID := seedModeration(t, f, ctx, target, reporter, "", 100)
		f.repKeeper.UnlockErr = gone
		defer func() { f.repKeeper.UnlockErr = nil }()
		lctx := late.WithMultiStore(ctx.MultiStore())
		require.NoError(t, f.keeper.ResolveUnappealedModerationInternal(lctx, target))
		has, err := f.keeper.DisplayNameReportStake.Has(lctx, reportID)
		require.NoError(t, err)
		require.False(t, has, "a gone lock settles and the record goes")
	})

	t.Run("appealed, zeroed reporter, both verdicts and the timeout", func(t *testing.T) {
		for _, run := range []func(sdk.Context) error{
			func(c sdk.Context) error { return f.keeper.ResolveDisplayNameAppealInternal(c, target, true) },
			func(c sdk.Context) error { return f.keeper.ResolveDisplayNameAppealInternal(c, target, false) },
			func(c sdk.Context) error {
				return f.keeper.ResolveAppealTimeoutInternal(c.WithBlockHeight(1_000_000_000), target)
			},
		} {
			ctx, _ := f.ctx.CacheContext()
			reportID := seedModeration(t, f, ctx, target, reporter, target, 100)
			require.NoError(t, hook.AfterMemberZeroed(ctx, reporter, []string{reporter}))
			require.NoError(t, run(ctx))
			m, err := f.keeper.DisplayNameModeration.Get(ctx, target)
			require.NoError(t, err)
			require.False(t, m.Active)
			has, err := f.keeper.DisplayNameReportStake.Has(ctx, reportID)
			require.NoError(t, err)
			require.False(t, has)
			has, err = f.keeper.DisplayNameAppealStake.Has(ctx, m.AppealChallengeId)
			require.NoError(t, err)
			require.False(t, has)
		}
	})

	t.Run("appealed, zeroed appellant", func(t *testing.T) {
		ctx, _ := f.ctx.CacheContext()
		seedModeration(t, f, ctx, target, reporter, target, 100)
		require.NoError(t, hook.AfterMemberZeroed(ctx, target, []string{target}))
		has, err := f.keeper.DisplayNameAppealStake.Has(ctx, "dna:"+target)
		require.NoError(t, err)
		require.False(t, has)
		require.NoError(t, f.keeper.ResolveDisplayNameAppealInternal(ctx, target, false))
	})
}

// SEA-102: a closed moderation that blocks a name survives history pruning;
// one without blocked names is still pruned.
func TestSEA102_BlockedNamesSurvivePruning(t *testing.T) {
	f := initBeginBlockFixture(t)
	blocked := TestAddrMember1.String()
	plain := TestAddrMember2.String()
	require.NoError(t, f.keeper.SetDisplayNameModeration(f.ctx, blocked, types.DisplayNameModeration{
		Member: blocked, RejectedName: "Rogue", ModeratedAt: 1, BlockedNames: []string{"rogue"},
	}))
	require.NoError(t, f.keeper.SetDisplayNameModeration(f.ctx, plain, types.DisplayNameModeration{
		Member: plain, RejectedName: "Fine", ModeratedAt: 1, AppealSucceeded: true,
	}))
	require.NoError(t, f.keeper.Season.Set(f.ctx, types.Season{Number: 1, EndBlock: 1 << 40, Status: types.SeasonStatus_SEASON_STATUS_ACTIVE}))
	ctx := f.ctx.WithBlockHeight(int64(90*24*60*10) + 100)
	require.NoError(t, f.keeper.BeginBlocker(ctx))

	_, err := f.keeper.DisplayNameModeration.Get(ctx, blocked)
	require.NoError(t, err, "the block is permanent")
	_, err = f.keeper.DisplayNameModeration.Get(ctx, plain)
	require.Error(t, err, "a closed case with no block is still pruned")
}

// SEA-104: the zeroing hook finds a member's nomination stakes through the
// by-staker index; the index follows every write.
func TestSEA104_NominationStakeStakerIndex(t *testing.T) {
	f := initBeginBlockFixture(t)
	for _, key := range []string{"1/cosmos1a", "2/cosmos1a", "2/cosmos1b"} {
		var id uint64
		var staker string
		_, err := fmt.Sscanf(key, "%d/%s", &id, &staker)
		require.NoError(t, err)
		require.NoError(t, f.keeper.NominationStake.Set(f.ctx, key, types.NominationStake{
			NominationId: id, Staker: staker, Amount: math.LegacyNewDec(10),
		}))
	}
	hook := keeper.NewZeroingHook(&f.keeper)
	require.NoError(t, hook.AfterMemberZeroed(f.ctx, "cosmos1a", []string{"cosmos1a"}))
	for key, want := range map[string]bool{"1/cosmos1a": false, "2/cosmos1a": false, "2/cosmos1b": true} {
		has, err := f.keeper.NominationStake.Has(f.ctx, key)
		require.NoError(t, err)
		require.Equal(t, want, has, key)
	}
	n := 0
	require.NoError(t, f.keeper.NominationStake.Indexes.ByStaker.Walk(f.ctx, nil, func(string, string) (bool, error) {
		n++
		return false, nil
	}))
	require.Equal(t, 1, n, "removed stakes leave the index")
}

// SEA-105: a stake above the uint64 range is refused with a clean error
// instead of panicking in Uint64().
func TestSEA105_OversizedNominationStakeRefused(t *testing.T) {
	f := initBeginBlockFixture(t)
	ms := keeper.NewMsgServerImpl(f.keeper)
	nominator, err := f.addressCodec.BytesToString(TestAddrCreator)
	require.NoError(t, err)
	staker, err := f.addressCodec.BytesToString(TestAddrMember1)
	require.NoError(t, err)
	ctx := f.ctx.WithBlockHeight(50)
	require.NoError(t, f.keeper.Season.Set(ctx, types.Season{
		Number: 1, EndBlock: 100000, Status: types.SeasonStatus_SEASON_STATUS_NOMINATION,
	}))
	require.NoError(t, f.keeper.SetNomination(ctx, types.Nomination{
		Id: 1, Nominator: nominator, ContentRef: "blog/post/1", Season: 1,
		TotalStaked: math.LegacyZeroDec(), Conviction: math.LegacyZeroDec(), RewardAmount: math.LegacyZeroDec(),
	}))
	f.repKeeper.SetMember(staker, 5000, nil, 0)

	require.NotPanics(t, func() {
		_, err = ms.StakeNomination(ctx, &types.MsgStakeNomination{
			Creator: staker, NominationId: 1, Amount: "100000000000000000000000",
		})
	})
	require.ErrorIs(t, err, types.ErrStakeAmountTooHigh)
}

package keeper_test

import (
	"testing"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	"github.com/stretchr/testify/require"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/types"
)

// FRM-27: zeroing a member closes their active conviction stakes without an
// unlock (the DREAM is already burned): they leave the active indexes, so
// accrual stops, and stay slashable as released stakes until GC.
func TestZeroingHookClosesConvictionStakes(t *testing.T) {
	f := initFixture(t)
	now := f.sdkCtx().BlockTime().Unix()
	stake := types.PostConvictionStake{
		Id: 7, Staker: testCreator2, PostId: 3, Amount: math.NewInt(10_000_000),
		StakedAt: now, UnlocksAt: now + 86400, LastAccrualAt: now,
	}
	require.NoError(t, f.keeper.PostConvictionStake.Set(f.ctx, stake.Id, stake))
	require.NoError(t, f.keeper.PostConvictionStakesByPost.Set(f.ctx, collections.Join(stake.PostId, stake.Id)))
	require.NoError(t, f.keeper.PostConvictionStakesByStaker.Set(f.ctx, collections.Join(stake.Staker, stake.Id)))

	unlocksBefore := len(f.repKeeper.unlockDreamCalls)

	hook := keeper.NewZeroingHook(&f.keeper)
	require.NoError(t, hook.AfterMemberZeroed(f.ctx, testCreator2, []string{testCreator2}))

	got, err := f.keeper.PostConvictionStake.Get(f.ctx, stake.Id)
	require.NoError(t, err)
	require.True(t, got.Released)
	for _, has := range []func() (bool, error){
		func() (bool, error) {
			return f.keeper.PostConvictionStakesByPost.Has(f.ctx, collections.Join(stake.PostId, stake.Id))
		},
		func() (bool, error) {
			return f.keeper.PostConvictionStakesByStaker.Has(f.ctx, collections.Join(stake.Staker, stake.Id))
		},
	} {
		ok, err := has()
		require.NoError(t, err)
		require.False(t, ok, "out of the active indexes")
	}
	ok, err := f.keeper.ReleasedStakesByPost.Has(f.ctx, collections.Join(stake.PostId, stake.Id))
	require.NoError(t, err)
	require.True(t, ok, "still slashable as a released stake")
	require.Len(t, f.repKeeper.unlockDreamCalls, unlocksBefore, "no unlock of burned DREAM")
}

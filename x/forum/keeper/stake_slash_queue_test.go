package keeper_test

import (
	"fmt"
	"testing"
	"time"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/forum/types"
)

// FRM-26: a standing hide on a heavily staked post settles its stakes in
// bounded batches through StakeSlashQueue, each stake exactly once, and the
// stakes can be neither released nor collected while the post is queued.
func TestSlashStakesForPost_BoundedQueueSlashesEachStakeOnce(t *testing.T) {
	f := initFixture(t)
	sdkCtx := sdk.UnwrapSDKContext(f.ctx).WithBlockTime(time.Unix(1_000_000, 0).UTC())
	f.ctx = sdkCtx

	author := sdk.AccAddress([]byte("slash-queue-author"))
	staker := sdk.AccAddress([]byte("slash-queue-staker"))
	postID := seedActivePostWithTags(t, f, author, []string{"backend"})

	const nActive, nReleased = 230, 30
	seed := func(id uint64, released bool) {
		stake := types.PostConvictionStake{
			Id:               id,
			PostId:           postID,
			Staker:           staker.String(),
			Amount:           math.NewInt(10_000_000),
			UnlocksAt:        0,
			Released:         released,
			AccruedRepPerTag: map[string]string{"backend": "1.000000000000000000"},
		}
		require.NoError(t, f.keeper.PostConvictionStake.Set(f.ctx, id, stake))
		if released {
			require.NoError(t, f.keeper.ReleasedStakesByPost.Set(f.ctx, collections.Join(postID, id)))
			// Already due for GC: only the queue guard keeps it alive.
			require.NoError(t, f.keeper.ReleasedStakeGc.Set(f.ctx, collections.Join(int64(0), id)))
			return
		}
		require.NoError(t, f.keeper.PostConvictionStakesByPost.Set(f.ctx, collections.Join(postID, id)))
		require.NoError(t, f.keeper.PostConvictionStakesByStaker.Set(f.ctx, collections.Join(staker.String(), id)))
	}
	for i := uint64(1); i <= nActive; i++ {
		seed(i, false)
	}
	for i := uint64(nActive + 1); i <= nActive+nReleased; i++ {
		seed(i, true)
	}

	slashed := func() map[string]int {
		counts := map[string]int{}
		for _, ev := range sdk.UnwrapSDKContext(f.ctx).EventManager().Events() {
			if ev.Type != "post_conviction_slashed" {
				continue
			}
			for _, a := range ev.Attributes {
				if a.Key == "stake_id" {
					counts[a.Value]++
				}
			}
		}
		return counts
	}

	// The tx path settles one batch and leaves the post queued.
	require.NoError(t, f.keeper.SlashStakesForPost(f.ctx, postID))
	require.Len(t, slashed(), 200)
	queued, err := f.keeper.StakeSlashQueue.Has(f.ctx, postID)
	require.NoError(t, err)
	require.True(t, queued, "post must stay queued until every stake is settled")

	// A stake still to be slashed can neither be released nor collected.
	var pending uint64
	require.NoError(t, f.keeper.PostConvictionStakesByPost.Walk(f.ctx,
		collections.NewPrefixedPairRange[uint64, uint64](postID),
		func(key collections.Pair[uint64, uint64]) (bool, error) {
			pending = key.K2()
			return true, nil
		}))
	require.NotZero(t, pending)
	err = f.keeper.ReleasePostConvictionStake(f.ctx, pending, staker)
	require.ErrorContains(t, err, "being slashed")
	require.NoError(t, f.keeper.GcReleasedPostConvictionStakes(f.ctx))

	// The EndBlocker finishes the post and drops it from the queue.
	require.NoError(t, f.keeper.ProcessStakeSlashQueue(f.ctx))
	counts := slashed()
	require.Len(t, counts, nActive+nReleased)
	for id, n := range counts {
		require.Equal(t, 1, n, "stake %s slashed %d times", id, n)
	}
	queued, err = f.keeper.StakeSlashQueue.Has(f.ctx, postID)
	require.NoError(t, err)
	require.False(t, queued)

	// Another pass is a no-op.
	require.NoError(t, f.keeper.ProcessStakeSlashQueue(f.ctx))
	require.Len(t, slashed(), nActive+nReleased)
	for i := uint64(1); i <= nActive; i++ {
		stake, err := f.keeper.PostConvictionStake.Get(f.ctx, i)
		require.NoError(t, err, fmt.Sprintf("active stake %d", i))
		require.True(t, stake.Released)
	}
}

func TestStakeSlashQueue_GenesisRoundTrip(t *testing.T) {
	f := initFixture(t)
	require.NoError(t, f.keeper.StakeSlashQueue.Set(f.ctx, 7, "author-seven"))
	gs, err := f.keeper.ExportGenesis(f.ctx)
	require.NoError(t, err)
	require.Equal(t, []types.PendingStakeSlash{{PostId: 7, Author: "author-seven"}}, gs.PendingStakeSlashList)

	dup := *gs
	dup.PendingStakeSlashList = append(dup.PendingStakeSlashList, types.PendingStakeSlash{PostId: 7})
	require.ErrorContains(t, dup.Validate(), "duplicate pending stake slash")

	g := initFixture(t)
	require.NoError(t, g.keeper.InitGenesis(g.ctx, *gs))
	author, err := g.keeper.StakeSlashQueue.Get(g.ctx, 7)
	require.NoError(t, err)
	require.Equal(t, "author-seven", author)
}

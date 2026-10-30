package keeper_test

import (
	"fmt"
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/types"
)

// REP-E-10: the per-epoch decay pass reads at most 500 records per block and
// resumes from a persisted cursor; every member is decayed exactly once and
// the epoch is marked done only when the pass completes.
func TestDecayPassIsBatchedAcrossBlocks(t *testing.T) {
	f := initFixture(t)
	k := f.keeper
	ctx := decayEpochContext(t, f)
	epoch, err := k.GetCurrentEpoch(ctx)
	require.NoError(t, err)

	const n = 1200
	addrs := make([]string, 0, n)
	for i := 0; i < n; i++ {
		addr := sdk.AccAddress([]byte(fmt.Sprintf("decay_pass_%09d", i))).String()
		addrs = append(addrs, addr)
		require.NoError(t, k.Member.Set(ctx, addr, types.Member{
			Address:        addr,
			DreamBalance:   PtrInt(math.NewInt(1_000_000_000)),
			StakedDream:    PtrInt(math.ZeroInt()),
			LifetimeEarned: PtrInt(math.ZeroInt()),
			LifetimeBurned: PtrInt(math.ZeroInt()),
			LastDecayEpoch: epoch - 1,
		}))
	}
	decayed := func() int {
		c := 0
		for _, a := range addrs {
			m, err := k.Member.Get(ctx, a)
			require.NoError(t, err)
			if m.LastDecayEpoch == epoch {
				c++
			}
		}
		return c
	}

	require.NoError(t, k.MaybeApplyBulkDecay(ctx))
	first := decayed()
	require.LessOrEqual(t, first, 500, "one block must not read more than the batch cap")
	require.Greater(t, first, 0)
	has, err := k.DecayLastProcessedEpoch.Has(ctx)
	require.NoError(t, err)
	if has {
		last, _ := k.DecayLastProcessedEpoch.Get(ctx)
		require.Less(t, last, uint64(epoch), "epoch must not be marked done mid-pass")
	}

	for i := 0; i < 10; i++ {
		require.NoError(t, k.MaybeApplyBulkDecay(ctx))
	}
	require.Equal(t, n, decayed())
	last, err := k.DecayLastProcessedEpoch.Get(ctx)
	require.NoError(t, err)
	require.Equal(t, uint64(epoch), last)
	running, err := k.DecayPassEpoch.Has(ctx)
	require.NoError(t, err)
	require.False(t, running)

	// Exactly once: a further call in the same epoch changes nothing.
	m, _ := k.Member.Get(ctx, addrs[0])
	require.NoError(t, k.MaybeApplyBulkDecay(ctx))
	m2, _ := k.Member.Get(ctx, addrs[0])
	require.Equal(t, m.DreamBalance.String(), m2.DreamBalance.String())
}

// REP-E-10: jury draws read reputable members from the max-reputation index,
// which follows every member write.
func TestJurorCandidatesFromReputationIndex(t *testing.T) {
	f := initFixture(t)
	k := f.keeper
	set := func(name, score string) string {
		addr := sdk.AccAddress([]byte(name)).String()
		scores := map[string]string{}
		if score != "" {
			scores["phoenix"] = score
		}
		require.NoError(t, k.Member.Set(f.ctx, addr, types.Member{Address: addr, ReputationScores: scores}))
		return addr
	}
	aurora := set("aurora______________", "120.5")
	zenith := set("zenith______________", "49.9")
	set("nobody______________", "")

	got, err := k.JurorCandidatesForTest(f.ctx, math.LegacyNewDec(50))
	require.NoError(t, err)
	require.Equal(t, []string{aurora}, got)

	// A reputation change moves the member in the index.
	set("zenith______________", "75")
	got, err = k.JurorCandidatesForTest(f.ctx, math.LegacyNewDec(50))
	require.NoError(t, err)
	require.ElementsMatch(t, []string{aurora, zenith}, got)

	// A removed member leaves the index.
	require.NoError(t, k.Member.Remove(f.ctx, aurora))
	got, err = k.JurorCandidatesForTest(f.ctx, math.LegacyNewDec(50))
	require.NoError(t, err)
	require.Equal(t, []string{zenith}, got)
}

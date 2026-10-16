package keeper_test

import (
	"testing"

	storetypes "cosmossdk.io/store/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/season/types"
)

// TestVoteHookGas reports the gas the vote hook consumes for a seeded
// genesis-style member (counters already high, a few achievements), first
// vote and tenth vote. Printed with -v; asserted against a ceiling so a
// regression that walks every definition again fails here instead of in
// the e2e suite where a 200000-gas vote tx goes out of gas.
func TestVoteHookGas(t *testing.T) {
	f, ctx := engineFixture(t)
	k := f.keeper
	voter := TestAddrMember1
	voterStr := GetAddrString(t, f.addressCodec, voter)
	require.NoError(t, k.MemberProfile.Set(ctx, voterStr, types.MemberProfile{
		Address: voterStr, SeasonXp: 5000, LifetimeXp: 5000, SeasonLevel: 8,
		VotesCast: 15, ForumHelpfulCount: 50, InvitationsSuccessful: 5, ChallengesWon: 2,
		Achievements:   []string{"first_step", "voice_heard", "contributor"},
		UnlockedTitles: []string{"newcomer", "veteran", "rising_star"},
	}))
	measure := func(proposal uint64) uint64 {
		gctx := ctx.WithGasMeter(storetypes.NewInfiniteGasMeter())
		require.NoError(t, k.OnVoteCast(gctx, voter, proposal))
		return gctx.GasMeter().GasConsumed()
	}
	first := measure(1)
	tenth := uint64(0)
	for p := uint64(2); p <= 10; p++ {
		tenth = measure(p)
	}
	t.Logf("vote hook gas: first=%d tenth=%d", first, tenth)
	require.Less(t, first, uint64(60000), "first vote hook gas")
	require.Less(t, tenth, uint64(40000), "steady-state vote hook gas")
}

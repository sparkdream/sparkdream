package keeper_test

import (
	"testing"
	"time"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/types"
)

// TestAppealDeadlineResolution covers the deadline pass of TimeoutExpiredAppeals:
// a quorum of seated jurors voting decisively resolves the appeal by verdict at
// the deadline; otherwise it times out. These exercise the partial-participation
// gap the vote-triggered path leaves open (votes cast below the supermajority
// trigger never tally on their own).
func TestAppealDeadlineResolution(t *testing.T) {
	advance := func(ctx sdk.Context) sdk.Context {
		return ctx.WithBlockTime(ctx.BlockTime().Add(
			time.Duration(types.DefaultAppealDeadline+10) * time.Second))
	}

	t.Run("quorum + decisive votes resolves by verdict at deadline", func(t *testing.T) {
		ja := setupJuryAppeal(t)
		// JurySize 5 -> supermajority trigger 4, deadline quorum 3.
		require.Equal(t, uint32(4), ja.review.RequiredVotes)

		// Cast 3 uphold votes: below the trigger (no early resolution) but at the
		// deadline quorum, and unanimous so the supermajority-of-cast passes.
		ja.vote(t, 3, types.Verdict_VERDICT_UPHOLD_CHALLENGE)

		_, appeal := findAppeal(t, ja.f, ja.appellantStr)
		require.Equal(t, types.GovAppealStatus_GOV_APPEAL_STATUS_PENDING, appeal.Status,
			"must still be pending before the deadline")

		require.NoError(t, ja.f.keeper.TimeoutExpiredAppeals(advance(ja.f.ctx)))

		_, updated := findAppeal(t, ja.f, ja.appellantStr)
		require.Equal(t, types.GovAppealStatus_GOV_APPEAL_STATUS_OVERTURNED, updated.Status)

		// Verdict economics, not timeout economics: full refund, no burn,
		// sentinel slashed, content reversed.
		require.Equal(t,
			math.NewInt(types.DefaultAppealBondAmount).String(),
			ja.refundedCoins.AmountOf("uspark").String())
		require.True(t, ja.burnedCoins.IsZero())
		up, ov := sumRoleVerdicts(t, ja.f, ja.sentinel)
		require.Zero(t, up)
		require.Equal(t, uint64(1), ov)
		require.Len(t, ja.fk.resolvedCalls, 1)
		require.Len(t, ja.fk.reverseCalls, 1)

		br, err := ja.f.keeper.BondedRoles.Get(ja.f.ctx, collections.Join(int32(types.RoleType_ROLE_TYPE_CONTENT_SENTINEL), ja.sentinel))
		require.NoError(t, err)
		require.Equal(t,
			math.NewInt(1_000_000_000).SubRaw(types.DefaultSentinelOverturnSlash).String(),
			br.CurrentBond)
	})

	t.Run("sub-quorum votes time out", func(t *testing.T) {
		ja := setupJuryAppeal(t)

		// 2 votes < deadline quorum (3): the jury did not show up enough to decide.
		ja.vote(t, 2, types.Verdict_VERDICT_UPHOLD_CHALLENGE)

		require.NoError(t, ja.f.keeper.TimeoutExpiredAppeals(advance(ja.f.ctx)))

		_, updated := findAppeal(t, ja.f, ja.appellantStr)
		require.Equal(t, types.GovAppealStatus_GOV_APPEAL_STATUS_TIMEOUT, updated.Status)

		// Timeout economics: half refund, half burn, no penalty / reversal.
		bond := math.NewInt(types.DefaultAppealBondAmount)
		half := bond.QuoRaw(2)
		require.Equal(t, bond.Sub(half).String(), ja.refundedCoins.AmountOf("uspark").String())
		require.Equal(t, half.String(), ja.burnedCoins.AmountOf("uspark").String())
		require.Empty(t, ja.fk.resolvedCalls)
		require.Empty(t, ja.fk.reverseCalls)
	})

	t.Run("no votes time out", func(t *testing.T) {
		ja := setupJuryAppeal(t)

		require.NoError(t, ja.f.keeper.TimeoutExpiredAppeals(advance(ja.f.ctx)))

		_, updated := findAppeal(t, ja.f, ja.appellantStr)
		require.Equal(t, types.GovAppealStatus_GOV_APPEAL_STATUS_TIMEOUT, updated.Status)
		require.Empty(t, ja.fk.resolvedCalls)
	})
}

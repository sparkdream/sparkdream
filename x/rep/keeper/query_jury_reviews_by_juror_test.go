package keeper_test

import (
	"testing"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

// Jury duty is drawn by lot, pays StandardComplexityBudget, and arrives without
// warning. Before this index there was no way for a juror to ask whether they
// were seated short of paging every review on the chain — and an unnoticed
// summons is the main way a jury loses quorum.
func TestJuryReviewsByJuror(t *testing.T) {
	f := initFixture(t)
	k, ctx := f.keeper, f.ctx
	qs := keeper.NewQueryServerImpl(k)

	alice := sdk.AccAddress([]byte("jbj-alice-address")).String()
	bob := sdk.AccAddress([]byte("jbj-bob-address--")).String()
	carol := sdk.AccAddress([]byte("jbj-carol-address")).String()

	seat := func(id uint64, verdict types.Verdict, jurors ...string) {
		review := types.JuryReview{Id: id, Jurors: jurors, Verdict: verdict}
		require.NoError(t, k.JuryReview.Set(ctx, id, review))
		require.NoError(t, k.AddJuryReviewToJurorIndex(ctx, review))
	}

	seat(1, types.Verdict_VERDICT_PENDING, alice, bob)
	seat(2, types.Verdict_VERDICT_REJECT_CHALLENGE, alice)
	seat(3, types.Verdict_VERDICT_PENDING, bob)

	t.Run("returns only that juror's seatings", func(t *testing.T) {
		res, err := qs.JuryReviewsByJuror(ctx, &types.QueryJuryReviewsByJurorRequest{Juror: alice})
		require.NoError(t, err)
		require.Len(t, res.JuryReview, 2)
		ids := []uint64{res.JuryReview[0].Id, res.JuryReview[1].Id}
		require.ElementsMatch(t, []uint64{1, 2}, ids)
	})

	t.Run("pending_only narrows to the summons still actionable", func(t *testing.T) {
		res, err := qs.JuryReviewsByJuror(ctx, &types.QueryJuryReviewsByJurorRequest{
			Juror: alice, PendingOnly: true,
		})
		require.NoError(t, err)
		require.Len(t, res.JuryReview, 1)
		require.Equal(t, uint64(1), res.JuryReview[0].Id)
	})

	t.Run("a juror with no seatings gets an empty list, not an error", func(t *testing.T) {
		res, err := qs.JuryReviewsByJuror(ctx, &types.QueryJuryReviewsByJurorRequest{Juror: carol})
		require.NoError(t, err)
		require.Empty(t, res.JuryReview)
	})

	t.Run("rejects an empty address", func(t *testing.T) {
		_, err := qs.JuryReviewsByJuror(ctx, &types.QueryJuryReviewsByJurorRequest{Juror: ""})
		require.Error(t, err)
	})
}

// A real seating must populate the index, not just a hand-built record.
func TestChallengeJurySeatingIsDiscoverable(t *testing.T) {
	f := initFixture(t)
	k, ctx := f.keeper, f.ctx
	qs := keeper.NewQueryServerImpl(k)

	initID, chalID, _ := challengedInitiative(t, k, ctx)

	pool := []string{}
	for _, name := range []string{"jbj-pool-alpha---", "jbj-pool-bravo---", "jbj-pool-charlie-", "jbj-pool-delta---", "jbj-pool-echo----"} {
		addr := sdk.AccAddress([]byte(name))
		mkFundedMember(t, k, ctx, addr, 0)
		m, err := k.GetMember(ctx, addr)
		require.NoError(t, err)
		m.ReputationScores = map[string]string{"tag": "500.0"}
		require.NoError(t, k.Member.Set(ctx, addr.String(), m))
		pool = append(pool, addr.String())
	}

	// Put the challenge back where CreateJuryReview expects it.
	challenge, err := k.GetChallenge(ctx, chalID)
	require.NoError(t, err)
	old := challenge.Status
	challenge.Status = types.ChallengeStatus_CHALLENGE_STATUS_ACTIVE
	require.NoError(t, k.Challenge.Set(ctx, chalID, challenge))
	require.NoError(t, k.UpdateChallengeStatusIndex(ctx, old, challenge.Status, chalID))

	require.NoError(t, k.CreateJuryReview(ctx, chalID, "response", nil))

	seatedSomeone := false
	for _, juror := range pool {
		res, err := qs.JuryReviewsByJuror(ctx, &types.QueryJuryReviewsByJurorRequest{
			Juror: juror, PendingOnly: true,
		})
		require.NoError(t, err)
		if len(res.JuryReview) > 0 {
			seatedSomeone = true
			require.Equal(t, initID, res.JuryReview[0].InitiativeId)
		}
	}
	require.True(t, seatedSomeone, "a seated juror must be able to find their own summons")
}

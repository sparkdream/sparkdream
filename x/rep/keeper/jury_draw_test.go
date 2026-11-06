package keeper_test

import (
	"crypto/sha256"
	"fmt"
	"slices"
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

// jurorPoolMember makes an ESTABLISHED, ACTIVE member with reputation in
// "tag", the tag challengedInitiative uses.
func jurorPoolMember(t *testing.T, k keeper.Keeper, ctx sdk.Context, label string) string {
	t.Helper()
	addr := sdk.AccAddress([]byte(fmt.Sprintf("%-20.20s", label)))
	mkFundedMember(t, k, ctx, addr, 0)
	m, err := k.GetMember(ctx, addr)
	require.NoError(t, err)
	m.Status = types.MemberStatus_MEMBER_STATUS_ACTIVE
	m.ReputationScores = map[string]string{"tag": "500.0"}
	require.NoError(t, k.Member.Set(ctx, addr.String(), m))
	return addr.String()
}

func setJuryParams(t *testing.T, k keeper.Keeper, ctx sdk.Context, size uint32) {
	t.Helper()
	params, err := k.Params.Get(ctx)
	require.NoError(t, err)
	params.JurySize = size
	params.MinJurorReputation = math.LegacyNewDec(50)
	require.NoError(t, k.Params.Set(ctx, params))
}

// REP-E-102: a refill after a decline used to skip only the current seats and
// the decliner, so the challenger, an expert witness or an earlier decliner
// could be drawn onto the case. Exclusion now lives in SelectJury, which
// reads the parties off the review.
func TestRedrawNeverSeatsAParty(t *testing.T) {
	f := initFixture(t)
	k, ctx := f.keeper, f.ctx
	setJuryParams(t, k, ctx, 3)

	initID, firstID, challenger := challengedInitiative(t, k, ctx)
	// A non-zero challenge id: expert witnesses need one.
	chal, err := k.GetChallenge(ctx, firstID)
	require.NoError(t, err)
	chalID := firstID + 100
	chal.Id = chalID
	require.NoError(t, k.Challenge.Set(ctx, chalID, chal))
	// The challenger is otherwise an eligible juror.
	m, err := k.GetMember(ctx, challenger)
	require.NoError(t, err)
	m.ReputationScores = map[string]string{"tag": "900.0"}
	require.NoError(t, k.Member.Set(ctx, challenger.String(), m))

	seated := []string{
		jurorPoolMember(t, k, ctx, "rdr-seat-a"),
		jurorPoolMember(t, k, ctx, "rdr-seat-b"),
		jurorPoolMember(t, k, ctx, "rdr-seat-c"),
	}
	expert := jurorPoolMember(t, k, ctx, "rdr-expert")
	earlierDecliner := jurorPoolMember(t, k, ctx, "rdr-old-decliner")
	honest := jurorPoolMember(t, k, ctx, "rdr-honest")

	review := types.JuryReview{
		Id: 60, ChallengeId: chalID, InitiativeId: initID,
		Jurors:             seated,
		ExpertWitnesses:    []string{expert},
		ExcludedJurors:     []string{earlierDecliner},
		Verdict:            types.Verdict_VERDICT_PENDING,
		Deadline:           ctx.BlockHeight() + 1000,
		AcceptanceDeadline: ctx.BlockHeight() + 100,
	}
	require.NoError(t, k.JuryReview.Set(ctx, review.Id, review))
	require.NoError(t, k.AddJuryReviewToVerdictIndex(ctx, review))
	require.NoError(t, k.AddJuryReviewToJurorIndex(ctx, review))

	// First decline: the only non-party left in the pool is drawn.
	require.NoError(t, k.DeclineJuryDuty(ctx, review.Id, seated[0]))
	mid, err := k.GetJuryReview(ctx, review.Id)
	require.NoError(t, err)
	require.Len(t, mid.Jurors, 2, "the seat is not refilled inside the decline")
	require.Equal(t, uint32(1), mid.PendingSeats)
	require.Contains(t, mid.ExcludedJurors, seated[0])
	require.NoError(t, k.DrawPendingJuries(ctx))

	after, err := k.GetJuryReview(ctx, review.Id)
	require.NoError(t, err)
	require.Equal(t, []string{seated[1], seated[2], honest}, after.Jurors)
	require.Zero(t, after.PendingSeats)

	// Second decline: everyone left in the pool is a party or a released
	// juror, so the seat stays empty rather than seating one of them.
	require.NoError(t, k.DeclineJuryDuty(ctx, review.Id, seated[1]))
	require.NoError(t, k.DrawPendingJuries(ctx))
	after, err = k.GetJuryReview(ctx, review.Id)
	require.NoError(t, err)
	require.Equal(t, []string{seated[2], honest}, after.Jurors)
	for _, party := range []string{challenger.String(), expert, earlierDecliner, seated[0], seated[1]} {
		require.NotContains(t, after.Jurors, party)
	}

	// Nor can a juror call a released juror as an expert witness.
	_, err = k.InviteExpertWitness(ctx, review.Id, sdk.MustAccAddressFromBech32(seated[2]), sdk.MustAccAddressFromBech32(seated[0]))
	require.ErrorIs(t, err, types.ErrExpertIneligible)
}

// REP-E-102, sweep path: the unanswered-seat sweep excluded only the kept and
// silent seats.
func TestSweepRedrawNeverSeatsAParty(t *testing.T) {
	f := initFixture(t)
	k, ctx := f.keeper, f.ctx
	setJuryParams(t, k, ctx, 5)

	initID, chalID, challenger := challengedInitiative(t, k, ctx)
	m, err := k.GetMember(ctx, challenger)
	require.NoError(t, err)
	m.ReputationScores = map[string]string{"tag": "900.0"}
	require.NoError(t, k.Member.Set(ctx, challenger.String(), m))

	var seated []string
	for _, l := range []string{"swr-a", "swr-b", "swr-c", "swr-d", "swr-e"} {
		seated = append(seated, jurorPoolMember(t, k, ctx, l))
	}
	expert := jurorPoolMember(t, k, ctx, "swr-expert")
	h1 := jurorPoolMember(t, k, ctx, "swr-honest-1")
	h2 := jurorPoolMember(t, k, ctx, "swr-honest-2")

	review := types.JuryReview{
		Id: 61, ChallengeId: chalID, InitiativeId: initID,
		Jurors:             seated,
		ExpertWitnesses:    []string{expert},
		Verdict:            types.Verdict_VERDICT_PENDING,
		Deadline:           ctx.BlockHeight() + 1000,
		AcceptanceDeadline: ctx.BlockHeight() + 10,
	}
	require.NoError(t, k.JuryReview.Set(ctx, review.Id, review))
	require.NoError(t, k.AddJuryReviewToVerdictIndex(ctx, review))
	require.NoError(t, k.AddJuryReviewToJurorIndex(ctx, review))
	require.NoError(t, k.AcceptJuryDuty(ctx, review.Id, seated[0]))
	require.NoError(t, k.AcceptJuryDuty(ctx, review.Id, seated[1]))
	require.NoError(t, k.AcceptJuryDuty(ctx, review.Id, seated[2]))

	swept := ctx.WithBlockHeight(ctx.BlockHeight() + 11)
	require.NoError(t, k.SweepUnansweredJurySeats(swept))
	require.NoError(t, k.DrawPendingJuries(swept))

	after, err := k.GetJuryReview(swept, review.Id)
	require.NoError(t, err)
	require.Len(t, after.Jurors, 5)
	require.Contains(t, after.Jurors, h1)
	require.Contains(t, after.Jurors, h2)
	for _, party := range []string{challenger.String(), expert, seated[3], seated[4]} {
		require.NotContains(t, after.Jurors, party)
	}
	require.Contains(t, after.ExcludedJurors, seated[3])
	require.Contains(t, after.ExcludedJurors, seated[4])
}

// REP-R-102: the assignee's response used to draw the jury inside its own
// transaction from the previous block's app hash, so the assignee could
// simulate the draw and send only when it suited them (or bundle a message
// that fails unless an accomplice is seated). The response now only queues
// the seats; with no beacon wired (seed_epoch 0, the floor) the EndBlocker
// draws them from the hash of the block itself. The beacon path is in
// jury_beacon_test.go.
func TestJuryIsDrawnInTheEndBlockerFromTheBlockHash(t *testing.T) {
	f := initFixture(t)
	k, ctx := f.keeper, f.ctx
	setJuryParams(t, k, ctx, 3)

	_, chalID, _ := challengedInitiative(t, k, ctx)
	for i := 0; i < 12; i++ {
		jurorPoolMember(t, k, ctx, fmt.Sprintf("seed-pool-%02d", i))
	}

	challenge, err := k.GetChallenge(ctx, chalID)
	require.NoError(t, err)
	old := challenge.Status
	challenge.Status = types.ChallengeStatus_CHALLENGE_STATUS_ACTIVE
	require.NoError(t, k.Challenge.Set(ctx, chalID, challenge))
	require.NoError(t, k.UpdateChallengeStatusIndex(ctx, old, challenge.Status, chalID))

	require.NoError(t, k.CreateJuryReview(ctx, chalID, "response", nil))

	var reviewID uint64
	require.NoError(t, k.JuryReview.Walk(ctx, nil, func(id uint64, r types.JuryReview) (bool, error) {
		if r.ChallengeId == chalID && r.InitiativeId != 0 {
			reviewID = id
			return true, nil
		}
		return false, nil
	}))
	review, err := k.GetJuryReview(ctx, reviewID)
	require.NoError(t, err)
	require.Empty(t, review.Jurors, "no juror is drawn inside the party's transaction")
	require.Equal(t, uint32(3), review.PendingSeats)
	queued, err := k.PendingJuryDraws.Get(ctx, reviewID)
	require.NoError(t, err)
	require.Zero(t, queued.SeedEpoch, "no beacon wired: block-hash seed, drawn in the next EndBlocker")

	draw := func(hash string) []string {
		cache, _ := ctx.CacheContext()
		h := sha256.Sum256([]byte(hash))
		cache = cache.WithHeaderHash(h[:]).WithEventManager(sdk.NewEventManager())
		require.NoError(t, k.DrawPendingJuries(cache))
		r, err := k.GetJuryReview(cache, reviewID)
		require.NoError(t, err)
		require.Len(t, r.Jurors, 3)
		require.Zero(t, r.PendingSeats)
		left, err := k.PendingJuryDraws.Has(cache, reviewID)
		require.NoError(t, err)
		require.False(t, left)
		src, ok := eventAttr(cache.EventManager().Events(), "jury_seated", "seed_source")
		require.True(t, ok)
		require.Equal(t, "block_hash", src)
		_, fellBack := eventAttr(cache.EventManager().Events(), "jury_seed_fallback", "reason")
		require.False(t, fellBack, "seed_epoch 0 is not a fallback")
		return r.Jurors
	}

	// Deterministic for a given block, different across blocks.
	require.Equal(t, draw("block-a"), draw("block-a"))
	distinct := map[string]bool{}
	for i := 0; i < 8; i++ {
		jurors := draw(fmt.Sprintf("block-%d", i))
		slices.Sort(jurors)
		distinct[fmt.Sprint(jurors)] = true
	}
	require.Greater(t, len(distinct), 1, "the block hash must drive the draw")
}

// Appeal-style cases record their parties on the review, so the draw (and
// any later one) excludes them.
func TestAppealJuryRecordsItsParties(t *testing.T) {
	f := initFixture(t)
	k, ctx := f.keeper, f.ctx
	for i := 0; i < 6; i++ {
		mkJurorMember(t, f, fmt.Sprintf("apl_juror_%02d", i), "100.0")
	}
	party := mkJurorMember(t, f, "apl_party", "900.0").String()

	id, err := k.CreateSeatedAppeal(ctx, "service.slash", []byte(`{}`), ctx.BlockHeight()+100, []string{party})
	require.NoError(t, err)
	review, err := k.GetJuryReview(ctx, id)
	require.NoError(t, err)
	require.Empty(t, review.Jurors)
	require.Contains(t, review.ExcludedJurors, party)

	require.NoError(t, k.DrawPendingJuries(ctx))
	review, err = k.GetJuryReview(ctx, id)
	require.NoError(t, err)
	require.NotEmpty(t, review.Jurors)
	require.NotContains(t, review.Jurors, party)
}

// A review exported with seats still pending (queued by a module whose
// EndBlocker runs after x/rep's) and no pending_jury_draw_list entry is
// queued again on import with the block-hash seed. The seed epoch round trip
// is TestPendingJuryDrawSeedEpochSurvivesGenesis.
func TestPendingJuryDrawSurvivesGenesis(t *testing.T) {
	f := initFixture(t)
	k, ctx := f.keeper, f.ctx

	review := types.JuryReview{Id: 5, Verdict: types.Verdict_VERDICT_PENDING, PendingSeats: 3, ExcludedJurors: []string{"x"}}
	require.NoError(t, k.JuryReview.Set(ctx, review.Id, review))
	out, err := k.ExportGenesis(ctx)
	require.NoError(t, err)

	g := initFixture(t)
	require.NoError(t, g.keeper.InitGenesis(g.ctx, *out))
	got, err := g.keeper.GetJuryReview(g.ctx, 5)
	require.NoError(t, err)
	require.Equal(t, uint32(3), got.PendingSeats)
	require.Equal(t, []string{"x"}, got.ExcludedJurors)
	queued, err := g.keeper.PendingJuryDraws.Get(g.ctx, 5)
	require.NoError(t, err)
	require.Equal(t, types.PendingJuryDraw{JuryReviewId: 5}, queued)
}

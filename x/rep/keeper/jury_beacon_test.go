package keeper_test

import (
	"context"
	"crypto/sha256"
	"fmt"
	"slices"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
	shieldtypes "sparkdream/x/shield/types"
)

// beaconStub stands in for x/shield's randomness beacon.
type beaconStub struct {
	epoch, interval uint64
	status          map[uint64]shieldtypes.BeaconStatus
	keys            map[uint64][]byte
}

func newBeaconStub(epoch, interval uint64) *beaconStub {
	return &beaconStub{epoch: epoch, interval: interval, status: map[uint64]shieldtypes.BeaconStatus{}, keys: map[uint64][]byte{}}
}

func (b *beaconStub) CurrentEpoch(context.Context) uint64        { return b.epoch }
func (b *beaconStub) BeaconEpochInterval(context.Context) uint64 { return b.interval }
func (b *beaconStub) BeaconStatus(_ context.Context, e uint64) shieldtypes.BeaconStatus {
	if _, ok := b.keys[e]; ok {
		return shieldtypes.BeaconStatus_BEACON_STATUS_AVAILABLE
	}
	if s, ok := b.status[e]; ok {
		return s
	}
	return shieldtypes.BeaconStatus_BEACON_STATUS_UNAVAILABLE
}
func (b *beaconStub) BeaconKey(_ context.Context, e uint64) ([]byte, bool) {
	k, ok := b.keys[e]
	return k, ok
}

// queuedChallengeReview opens a challenge response so its jury draw is
// queued, and returns the review id. A 12-member pool with "tag" reputation,
// jury size 3.
func queuedChallengeReview(t *testing.T, k keeper.Keeper, ctx sdk.Context) uint64 {
	t.Helper()
	return queuedChallengeReviewSized(t, k, ctx, 3)
}

func queuedChallengeReviewSized(t *testing.T, k keeper.Keeper, ctx sdk.Context, size uint32) uint64 {
	t.Helper()
	setJuryParams(t, k, ctx, size)
	_, chalID, _ := challengedInitiative(t, k, ctx)
	for i := 0; i < 12; i++ {
		jurorPoolMember(t, k, ctx, fmt.Sprintf("bcn-pool-%02d", i))
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
	return reviewID
}

func eventAttr(events sdk.Events, typ, key string) (string, bool) {
	for _, ev := range events {
		if ev.Type != typ {
			continue
		}
		for _, a := range ev.Attributes {
			if a.Key == key {
				return a.Value, true
			}
		}
	}
	return "", false
}

func beaconKey(label string) []byte {
	h := sha256.Sum256([]byte(label))
	return h[:]
}

// Opening with the beacon wired records the epoch after the current one as
// the seed epoch; without a beacon value in prospect, 0 (block-hash seed).
func TestJuryDrawRecordsBeaconSeedEpoch(t *testing.T) {
	t.Run("beacon pending for the next epoch", func(t *testing.T) {
		f := initFixture(t)
		k, ctx := f.keeper, f.ctx.WithBlockHeight(77)
		b := newBeaconStub(10, 5)
		b.status[11] = shieldtypes.BeaconStatus_BEACON_STATUS_PENDING
		k.SetBeaconSource(b)

		id := queuedChallengeReview(t, k, ctx)
		draw, err := k.PendingJuryDraws.Get(ctx, id)
		require.NoError(t, err)
		require.Equal(t, types.PendingJuryDraw{JuryReviewId: id, SeedEpoch: 11, QueuedAtHeight: 77}, draw)
	})
	t.Run("beacon unavailable", func(t *testing.T) {
		f := initFixture(t)
		k, ctx := f.keeper, f.ctx
		k.SetBeaconSource(newBeaconStub(10, 5)) // no DKG: every epoch UNAVAILABLE
		id := queuedChallengeReview(t, k, ctx)
		draw, err := k.PendingJuryDraws.Get(ctx, id)
		require.NoError(t, err)
		require.Zero(t, draw.SeedEpoch)
	})
	t.Run("beacon unwired", func(t *testing.T) {
		f := initFixture(t)
		k, ctx := f.keeper, f.ctx
		id := queuedChallengeReview(t, k, ctx)
		draw, err := k.PendingJuryDraws.Get(ctx, id)
		require.NoError(t, err)
		require.Zero(t, draw.SeedEpoch)
	})
}

// A queued draw waits while its beacon value is PENDING, then seats from the
// value: the same key always seats the same jury whatever the block hash,
// and a different key draws differently.
func TestJuryDrawWaitsForBeaconThenSeedsFromIt(t *testing.T) {
	f := initFixture(t)
	k, ctx := f.keeper, f.ctx
	b := newBeaconStub(10, 1)
	b.status[11] = shieldtypes.BeaconStatus_BEACON_STATUS_PENDING
	k.SetBeaconSource(b)
	id := queuedChallengeReview(t, k, ctx)

	// PENDING: nothing seated, the entry stays, no fallback.
	ctx = ctx.WithEventManager(sdk.NewEventManager())
	require.NoError(t, k.DrawPendingJuries(ctx))
	review, err := k.GetJuryReview(ctx, id)
	require.NoError(t, err)
	require.Empty(t, review.Jurors)
	require.Equal(t, uint32(3), review.PendingSeats)
	queued, err := k.PendingJuryDraws.Has(ctx, id)
	require.NoError(t, err)
	require.True(t, queued)
	_, fellBack := eventAttr(ctx.EventManager().Events(), "jury_seed_fallback", "reason")
	require.False(t, fellBack)

	draw := func(key []byte, hash string) []string {
		b.keys[11] = key
		defer delete(b.keys, 11)
		cache, _ := ctx.CacheContext()
		h := sha256.Sum256([]byte(hash))
		cache = cache.WithHeaderHash(h[:]).WithEventManager(sdk.NewEventManager())
		require.NoError(t, k.DrawPendingJuries(cache))
		r, err := k.GetJuryReview(cache, id)
		require.NoError(t, err)
		require.Len(t, r.Jurors, 3)
		require.Zero(t, r.PendingSeats)
		left, err := k.PendingJuryDraws.Has(cache, id)
		require.NoError(t, err)
		require.False(t, left)
		src, ok := eventAttr(cache.EventManager().Events(), "jury_seated", "seed_source")
		require.True(t, ok)
		require.Equal(t, "beacon", src)
		_, fellBack := eventAttr(cache.EventManager().Events(), "jury_seed_fallback", "reason")
		require.False(t, fellBack)
		return r.Jurors
	}

	// A pure function of (key, review id): the block hash plays no part.
	require.Equal(t, draw(beaconKey("key-a"), "block-1"), draw(beaconKey("key-a"), "block-2"))
	distinct := map[string]bool{}
	for i := 0; i < 8; i++ {
		jurors := draw(beaconKey(fmt.Sprintf("key-%d", i)), "block-1")
		slices.Sort(jurors)
		distinct[fmt.Sprint(jurors)] = true
	}
	require.Greater(t, len(distinct), 1, "the beacon value must drive the draw")
}

// A beacon that stays PENDING past JuryBeaconWaitBlocks falls back to the
// block-hash seed with a jury_seed_fallback event; one that became
// UNAVAILABLE (a re-key retired the epoch) falls back at once.
func TestJuryDrawFallsBackFromBeacon(t *testing.T) {
	t.Run("wait exceeded", func(t *testing.T) {
		f := initFixture(t)
		k, ctx := f.keeper, f.ctx.WithBlockHeight(100)
		b := newBeaconStub(10, 2)
		b.status[11] = shieldtypes.BeaconStatus_BEACON_STATUS_PENDING
		k.SetBeaconSource(b)
		id := queuedChallengeReview(t, k, ctx)
		wait := types.JuryBeaconWaitBlocks(2)
		require.Equal(t, int64(2)*int64(types.JuryBeaconWaitEpochs), wait)

		// At the limit it still waits.
		at := ctx.WithBlockHeight(100 + wait).WithEventManager(sdk.NewEventManager())
		require.NoError(t, k.DrawPendingJuries(at))
		queued, err := k.PendingJuryDraws.Has(at, id)
		require.NoError(t, err)
		require.True(t, queued)

		// One block past it, the block hash seats the jury.
		past := ctx.WithBlockHeight(101 + wait).WithEventManager(sdk.NewEventManager())
		require.NoError(t, k.DrawPendingJuries(past))
		review, err := k.GetJuryReview(past, id)
		require.NoError(t, err)
		require.Len(t, review.Jurors, 3)
		queued, err = k.PendingJuryDraws.Has(past, id)
		require.NoError(t, err)
		require.False(t, queued)
		reason, ok := eventAttr(past.EventManager().Events(), "jury_seed_fallback", "reason")
		require.True(t, ok)
		require.Equal(t, "wait_exceeded", reason)
		epoch, _ := eventAttr(past.EventManager().Events(), "jury_seed_fallback", "seed_epoch")
		require.Equal(t, "11", epoch)
		src, _ := eventAttr(past.EventManager().Events(), "jury_seated", "seed_source")
		require.Equal(t, "block_hash", src)
	})
	t.Run("unavailable", func(t *testing.T) {
		f := initFixture(t)
		k, ctx := f.keeper, f.ctx
		b := newBeaconStub(10, 2)
		b.status[11] = shieldtypes.BeaconStatus_BEACON_STATUS_PENDING
		k.SetBeaconSource(b)
		id := queuedChallengeReview(t, k, ctx)
		delete(b.status, 11) // retired: UNAVAILABLE
		ctx = ctx.WithEventManager(sdk.NewEventManager())
		require.NoError(t, k.DrawPendingJuries(ctx))
		review, err := k.GetJuryReview(ctx, id)
		require.NoError(t, err)
		require.Len(t, review.Jurors, 3)
		reason, ok := eventAttr(ctx.EventManager().Events(), "jury_seed_fallback", "reason")
		require.True(t, ok)
		require.Equal(t, "unavailable", reason)
	})
}

// A refill (decline) on a review already waiting joins the queued draw and
// keeps its seed epoch.
func TestJuryRefillKeepsQueuedSeedEpoch(t *testing.T) {
	f := initFixture(t)
	k, ctx := f.keeper, f.ctx.WithBlockHeight(50)
	b := newBeaconStub(10, 1)
	b.status[11] = shieldtypes.BeaconStatus_BEACON_STATUS_PENDING
	k.SetBeaconSource(b)
	id := queuedChallengeReview(t, k, ctx)

	b.epoch = 12 // epochs moved on while the draw waited
	review, err := k.GetJuryReview(ctx, id)
	require.NoError(t, err)
	review.Jurors = []string{jurorPoolMember(t, k, ctx, "bcn-seated")}
	require.NoError(t, k.JuryReview.Set(ctx, id, review))
	require.NoError(t, k.AddJuryReviewToJurorIndex(ctx, review))
	require.NoError(t, k.DeclineJuryDuty(ctx.WithBlockHeight(60), id, review.Jurors[0]))

	draw, err := k.PendingJuryDraws.Get(ctx, id)
	require.NoError(t, err)
	require.Equal(t, uint64(11), draw.SeedEpoch)
	require.Equal(t, int64(50), draw.QueuedAtHeight)
	review, err = k.GetJuryReview(ctx, id)
	require.NoError(t, err)
	require.Equal(t, uint32(4), review.PendingSeats)
}

// pending_jury_draw_list round-trips the seed epoch; a review with pending
// seats and no entry is queued with seed epoch 0.
func TestPendingJuryDrawSeedEpochSurvivesGenesis(t *testing.T) {
	f := initFixture(t)
	k, ctx := f.keeper, f.ctx
	for _, r := range []types.JuryReview{
		{Id: 5, Verdict: types.Verdict_VERDICT_PENDING, PendingSeats: 3},
		{Id: 6, Verdict: types.Verdict_VERDICT_PENDING, PendingSeats: 1},
	} {
		require.NoError(t, k.JuryReview.Set(ctx, r.Id, r))
	}
	require.NoError(t, k.JuryReviewSeq.Set(ctx, 7))
	require.NoError(t, k.PendingJuryDraws.Set(ctx, 5, types.PendingJuryDraw{JuryReviewId: 5, SeedEpoch: 42, QueuedAtHeight: 900}))
	out, err := k.ExportGenesis(ctx)
	require.NoError(t, err)
	require.Equal(t, []types.PendingJuryDraw{{JuryReviewId: 5, SeedEpoch: 42, QueuedAtHeight: 900}}, out.PendingJuryDrawList)
	require.NoError(t, out.Validate())

	// Imported at a height the draw is already behind (no beacon wired), so
	// the REP-R-206 clamp leaves it alone.
	g := initFixture(t)
	g.ctx = g.ctx.WithBlockHeight(950)
	require.NoError(t, g.keeper.InitGenesis(g.ctx, *out))
	got, err := g.keeper.PendingJuryDraws.Get(g.ctx, 5)
	require.NoError(t, err)
	require.Equal(t, types.PendingJuryDraw{JuryReviewId: 5, SeedEpoch: 42, QueuedAtHeight: 900}, got)
	got, err = g.keeper.PendingJuryDraws.Get(g.ctx, 6)
	require.NoError(t, err)
	require.Equal(t, types.PendingJuryDraw{JuryReviewId: 6}, got)

	// Validate refuses an entry for an unknown review and a duplicate.
	bad := *out
	bad.PendingJuryDrawList = append(bad.PendingJuryDrawList, types.PendingJuryDraw{JuryReviewId: 99})
	require.Error(t, bad.Validate())
	bad = *out
	bad.PendingJuryDrawList = append(bad.PendingJuryDrawList, types.PendingJuryDraw{JuryReviewId: 5})
	require.Error(t, bad.Validate())
}

// REP-R-201: the acceptance window used to be fixed when the review opened.
// With the beacon pending past it, the sweep found no seated juror, stood
// the clock down for good, and the jurors seated later were never swept for
// silence. The draw now restarts the window from the block it seats.
func TestAcceptanceWindowRestartsWhenTheDrawSeats(t *testing.T) {
	f := initFixture(t)
	k, ctx := f.keeper, f.ctx.WithBlockHeight(100)
	// A long epoch interval keeps JuryBeaconWaitBlocks well past the
	// acceptance window, so the draw genuinely waits instead of falling back.
	b := newBeaconStub(10, 100_000)
	b.status[11] = shieldtypes.BeaconStatus_BEACON_STATUS_PENDING
	k.SetBeaconSource(b)
	id := queuedChallengeReviewSized(t, k, ctx, 5)

	opened, err := k.GetJuryReview(ctx, id)
	require.NoError(t, err)
	window := opened.AcceptanceDeadline - ctx.BlockHeight()
	require.Positive(t, window)

	// The window elapses while the beacon is still pending: the sweep finds
	// nobody to charge and stands the clock down; the draw stays queued.
	stale := ctx.WithBlockHeight(opened.AcceptanceDeadline)
	require.NoError(t, k.SweepUnansweredJurySeats(stale))
	require.NoError(t, k.DrawPendingJuries(stale))
	review, err := k.GetJuryReview(stale, id)
	require.NoError(t, err)
	require.Empty(t, review.Jurors)
	require.Zero(t, review.AcceptanceDeadline)

	// The beacon value lands: the draw seats five and restarts the window
	// from the seating block.
	b.keys[11] = beaconKey("late-key")
	seatedAt := stale.WithBlockHeight(stale.BlockHeight() + 1)
	require.NoError(t, k.DrawPendingJuries(seatedAt))
	review, err = k.GetJuryReview(seatedAt, id)
	require.NoError(t, err)
	require.Len(t, review.Jurors, 5)
	require.Equal(t, seatedAt.BlockHeight()+window, review.AcceptanceDeadline,
		"the acceptance window counts from the seating, not from the opening")

	// Nobody answers: the sweep still vacates the silent seats (down to the
	// MinSeatedJurors floor), charges them and queues replacements.
	swept := seatedAt.WithBlockHeight(review.AcceptanceDeadline)
	require.NoError(t, k.SweepUnansweredJurySeats(swept))
	require.NoError(t, k.DrawPendingJuries(swept))
	after, err := k.GetJuryReview(swept, id)
	require.NoError(t, err)
	require.Equal(t, uint32(1), after.RedrawCount, "the silent seats were swept")
	vacated := 0
	for _, juror := range review.Jurors {
		if !slices.Contains(after.Jurors, juror) {
			vacated++
			p, err := k.GetJuryParticipation(swept, juror)
			require.NoError(t, err)
			require.Equal(t, uint64(1), p.TotalTimeouts)
		}
	}
	require.Equal(t, 2, vacated, "5 seats less the MinSeatedJurors floor")
}

// REP-R-206: a pending draw imported from a chain that was further along
// carries a seed epoch and a queued height ahead of this chain. Both are
// clamped at import so the draw neither waits for a future epoch nor
// measures its fallback wait from a height not yet reached.
func TestImportedJuryDrawIsClampedToTheChain(t *testing.T) {
	exported := func(t *testing.T) types.GenesisState {
		t.Helper()
		f := initFixture(t)
		review := types.JuryReview{Id: 5, Verdict: types.Verdict_VERDICT_PENDING, PendingSeats: 3}
		require.NoError(t, f.keeper.JuryReview.Set(f.ctx, review.Id, review))
		require.NoError(t, f.keeper.PendingJuryDraws.Set(f.ctx, 5,
			types.PendingJuryDraw{JuryReviewId: 5, SeedEpoch: 900, QueuedAtHeight: 1_000_000}))
		out, err := f.keeper.ExportGenesis(f.ctx)
		require.NoError(t, err)
		require.Len(t, out.PendingJuryDrawList, 1)
		return *out
	}
	imported := func(t *testing.T, g *fixture, gen types.GenesisState) types.PendingJuryDraw {
		t.Helper()
		g.ctx = g.ctx.WithBlockHeight(5)
		require.NoError(t, g.keeper.InitGenesis(g.ctx, gen))
		draw, err := g.keeper.PendingJuryDraws.Get(g.ctx, 5)
		require.NoError(t, err)
		return draw
	}

	t.Run("beacon pending for the next epoch", func(t *testing.T) {
		gen := exported(t)
		g := initFixture(t)
		b := newBeaconStub(10, 1)
		b.status[11] = shieldtypes.BeaconStatus_BEACON_STATUS_PENDING
		g.keeper.SetBeaconSource(b)
		require.Equal(t, types.PendingJuryDraw{JuryReviewId: 5, SeedEpoch: 11, QueuedAtHeight: 5}, imported(t, g, gen))

		// And the draw runs as soon as that epoch's value lands.
		b.keys[11] = beaconKey("fresh-chain")
		require.NoError(t, g.keeper.DrawPendingJuries(g.ctx))
		queued, err := g.keeper.PendingJuryDraws.Has(g.ctx, 5)
		require.NoError(t, err)
		require.False(t, queued, "the imported draw must not stall")
	})
	t.Run("no beacon value in prospect", func(t *testing.T) {
		gen := exported(t)
		g := initFixture(t)
		g.keeper.SetBeaconSource(newBeaconStub(10, 1)) // no DKG: every epoch UNAVAILABLE
		require.Equal(t, types.PendingJuryDraw{JuryReviewId: 5, QueuedAtHeight: 5}, imported(t, g, gen))
	})
	t.Run("beacon unwired", func(t *testing.T) {
		gen := exported(t)
		g := initFixture(t)
		got := imported(t, g, gen)
		require.Equal(t, int64(5), got.QueuedAtHeight, "the fallback wait runs from the import")
		require.Equal(t, uint64(900), got.SeedEpoch, "nothing to clamp the epoch against")
	})
	t.Run("a draw already behind the chain is untouched", func(t *testing.T) {
		gen := exported(t)
		gen.PendingJuryDrawList[0] = types.PendingJuryDraw{JuryReviewId: 5, SeedEpoch: 3, QueuedAtHeight: 2}
		g := initFixture(t)
		g.keeper.SetBeaconSource(newBeaconStub(10, 1))
		require.Equal(t, types.PendingJuryDraw{JuryReviewId: 5, SeedEpoch: 3, QueuedAtHeight: 2}, imported(t, g, gen))
	})
}

package keeper

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"maps"
	"math/big"
	"math/rand"
	"slices"
	"strings"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	commontypes "sparkdream/x/common/types"
	"sparkdream/x/rep/types"
	shieldtypes "sparkdream/x/shield/types"
)

// Jury draws.
//
// Two rules hold for every jury this module seats (REP-R-102, REP-E-102):
//
//   - **No draw inside a party's transaction, and no seed a validator can
//     choose.** Opening a review (an assignee's response, an appeal, a
//     migration objection, an external case) or refilling it (a decline)
//     only records how many seats are wanted (JuryReview.pending_seats) and
//     queues a PendingJuryDraw naming the x/shield beacon epoch that will
//     seed it: the epoch after the one open at queueing time. Its beacon
//     value, the epoch's TLE key (a threshold BLS signature over the epoch
//     number), does not exist until the epoch has closed and a threshold of
//     validators has released shares, so neither the sender nor any block
//     proposer nor any validator short of the threshold knows the seed when
//     the review opens, and no one can bias it (the value is unique).
//     DrawPendingJuries seats the review in the first BeginBlocker that
//     finds the value AVAILABLE, before that block's transactions, so the
//     juror pool it draws from was settled before anyone could compute the
//     outcome. Without a beacon (no DKG yet, beacon disabled, or x/shield
//     unwired) the draw is seeded from the hash of the block being
//     finalized in the next BeginBlocker, which the sender cannot know when
//     signing but a block proposer can bias; a beacon that stays PENDING
//     past JuryBeaconWaitBlocks falls back the same way (event
//     jury_seed_fallback). See "Jury Selection" in docs/x-rep-spec.md.
//   - **The case decides who is excluded, never the caller.** SelectJury
//     derives the parties from the review itself: the initiative challenger
//     and every initiative affiliate, a content challenge's challenger and
//     author, the expert witnesses, the parties recorded when the case opened
//     and every juror who declined or was vacated (excluded_jurors). All are
//     compared as canonical members.

// jurorCandidate is one eligible juror and their integer selection weight:
// the LegacyDec weight's 18-decimal integer representation, so the draw uses
// no floating point (REP-R-107).
type jurorCandidate struct {
	addr   string
	weight *big.Int
}

// juryDrawSeedDomain separates the block-hash jury-draw seed from any other
// use of the block hash; juryBeaconSeedDomain does the same for the beacon
// value.
const (
	juryDrawSeedDomain   = "sparkdream/jury-draw/v1"
	juryBeaconSeedDomain = "sparkdream/jury-draw/beacon/v1"

	// Values of the jury_seated seed_source attribute and the
	// jury_seed_fallback reason attribute.
	seedSourceBeacon     = "beacon"
	seedSourceBlockHash  = "block_hash"
	fallbackUnavailable  = "unavailable"
	fallbackWaitExceeded = "wait_exceeded"
)

// juryBeaconRng returns the deterministic PRNG for drawing seats on reviewID
// from the beacon value key: SHA-256 over the domain, the key and the review
// id, so two reviews seeded from one epoch do not share a stream. A pure
// function of (key, reviewID): the same key always seats the same jury.
func juryBeaconRng(key []byte, reviewID uint64) *rand.Rand {
	h := sha256.New()
	h.Write([]byte(juryBeaconSeedDomain))
	h.Write(key)
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], reviewID)
	h.Write(buf[:])
	sum := h.Sum(nil)
	return rand.New(rand.NewSource(int64(binary.BigEndian.Uint64(sum[:8])))) //nolint:gosec // seed bits; deterministic PRNG is required for consensus
}

// juryDrawRng returns the deterministic PRNG for drawing seats on reviewID in
// this block: SHA-256 over the domain, the hash of the block being finalized
// (unknown to anyone signing a transaction for it), the previous app hash,
// the height and the review id, so two reviews drawn in one block do not share
// a stream. Unit-test contexts carry no header hash and fall back to the rest.
func (k Keeper) juryDrawRng(ctx context.Context, reviewID uint64) *rand.Rand {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	h := sha256.New()
	h.Write([]byte(juryDrawSeedDomain))
	h.Write(sdkCtx.HeaderHash())
	h.Write(sdkCtx.BlockHeader().AppHash)
	var buf [16]byte
	binary.BigEndian.PutUint64(buf[:8], uint64(sdkCtx.BlockHeight())) //nolint:gosec // seed bits
	binary.BigEndian.PutUint64(buf[8:], reviewID)
	h.Write(buf[:])
	sum := h.Sum(nil)
	return rand.New(rand.NewSource(int64(binary.BigEndian.Uint64(sum[:8])))) //nolint:gosec // seed bits; deterministic PRNG is required for consensus
}

// candidateWeight turns a reputation total and the juror's responsiveness
// multiplier into an integer weight.
func (k Keeper) candidateWeight(ctx context.Context, addr string, totalRep math.LegacyDec) *big.Int {
	w := totalRep.Mul(k.JurorResponsivenessWeight(ctx, addr))
	if !w.IsPositive() {
		return new(big.Int)
	}
	return new(big.Int).Set(w.BigInt())
}

// weightedPick draws one index with probability proportional to its weight;
// uniformly when every weight is zero.
func weightedPick(rng *rand.Rand, pool []jurorCandidate) int {
	total := new(big.Int)
	for _, c := range pool {
		total.Add(total, c.weight)
	}
	if total.Sign() <= 0 {
		return rng.Intn(len(pool))
	}
	r := new(big.Int).Rand(rng, total) // [0, total)
	sum := new(big.Int)
	for i, c := range pool {
		sum.Add(sum, c.weight)
		if r.Cmp(sum) < 0 {
			return i
		}
	}
	return len(pool) - 1
}

// drawJurors draws up to n distinct jurors from pool, weighted, without
// replacement.
func drawJurors(rng *rand.Rand, pool []jurorCandidate, n int) []string {
	if n > len(pool) {
		n = len(pool)
	}
	pool = slices.Clone(pool)
	out := make([]string, 0, n)
	for i := 0; i < n; i++ {
		idx := weightedPick(rng, pool)
		out = append(out, pool[idx].addr)
		pool = slices.Delete(pool, idx, idx+1)
	}
	return out
}

// isInitiativeJury reports whether review is an initiative-challenge jury.
// Content-challenge reviews and appeal / external cases carry InitiativeId 0.
func isInitiativeJury(review types.JuryReview) bool {
	return review.InitiativeId != 0
}

// isAppealJury reports whether review was opened through
// CreateAppealInitiative (gov-action appeals, migration objections, external
// cases): neither an initiative nor a content challenge.
func isAppealJury(review types.JuryReview) bool {
	return review.InitiativeId == 0 && review.ContentChallengeId == 0
}

// juryExclusions is the canonical set of members who may never be drawn onto
// review: everyone already seated, the expert witnesses, the recorded parties
// and released jurors, and the parties the case itself names.
func (k Keeper) juryExclusions(ctx context.Context, review types.JuryReview) map[string]struct{} {
	skip := make([]string, 0, len(review.Jurors)+len(review.ExpertWitnesses)+len(review.ExcludedJurors)+6)
	skip = append(skip, review.Jurors...)
	skip = append(skip, review.ExpertWitnesses...)
	skip = append(skip, review.ExcludedJurors...)
	switch {
	case isInitiativeJury(review):
		if challenge, err := k.Challenge.Get(ctx, review.ChallengeId); err == nil {
			skip = append(skip, challenge.Challenger)
		}
		if initiative, err := k.GetInitiative(ctx, review.InitiativeId); err == nil {
			if project, pErr := k.GetProject(ctx, initiative.ProjectId); pErr == nil {
				skip = append(skip, InitiativeAffiliates(initiative, project)...)
			} else {
				skip = append(skip, initiative.Assignee, initiative.Apprentice, initiative.Creator)
			}
		}
	case review.ContentChallengeId != 0:
		if cc, err := k.ContentChallenge.Get(ctx, review.ContentChallengeId); err == nil {
			skip = append(skip, cc.Challenger, cc.Author)
		}
	}
	return k.canonicalSet(ctx, skip...)
}

// excludedAsExpert reports whether expert may not be called as an expert
// witness on review: anyone juryExclusions names, plus every invitation
// neighbour (inviter or invitee) of anyone it names (REP-E-103). Expert pay is
// directed by one juror, so without the neighbour rule a juror could call
// their own invitees, and a challenger or assignee could be called through
// theirs. Jury draws do not apply the neighbour rule: they are random, and
// the pool is often small.
func (k Keeper) excludedAsExpert(ctx context.Context, review types.JuryReview, expert string) bool {
	excl := k.juryExclusions(ctx, review)
	canon := k.Canonical(ctx, expert)
	if _, ok := excl[canon]; ok {
		return true
	}
	if m, err := k.Member.Get(ctx, canon); err == nil && m.InvitedBy != "" {
		if _, ok := excl[k.Canonical(ctx, m.InvitedBy)]; ok {
			return true
		}
	}
	// Sorted, so every node reads the same records in the same order and
	// meters the same gas.
	for _, p := range slices.Sorted(maps.Keys(excl)) {
		if m, err := k.Member.Get(ctx, p); err == nil && m.InvitedBy != "" && k.SameMember(ctx, m.InvitedBy, canon) {
			return true
		}
	}
	return false
}

// jurorPool returns the eligible, non-excluded jurors for review with their
// weights. Initiative juries draw from members with at least
// MinJurorReputation in one of the initiative's tags, weighted by their
// reputation in those tags. Every other jury draws from members with
// MinJurorReputation in any tag, weighted by their total reputation. Both are
// discounted by JurorResponsivenessWeight.
func (k Keeper) jurorPool(ctx context.Context, review types.JuryReview) ([]jurorCandidate, error) {
	params, err := k.Params.Get(ctx)
	if err != nil {
		return nil, err
	}
	minReputation := params.MinJurorReputation
	exclude := k.juryExclusions(ctx, review)

	var tags []string
	if isInitiativeJury(review) {
		initiative, iErr := k.GetInitiative(ctx, review.InitiativeId)
		if iErr != nil {
			return nil, iErr
		}
		tags = initiative.Tags
	}

	var pool []jurorCandidate
	err = k.walkJurorCandidates(ctx, minReputation, func(addr string, member types.Member) error {
		if _, skip := exclude[addr]; skip {
			return nil
		}
		// Only an ACTIVE member may be seated. Zeroed members fall out of the
		// index when their scores are cleared and tombstones have no Member
		// row, so this is a belt for the status the record itself carries.
		if member.Status != types.MemberStatus_MEMBER_STATUS_ACTIVE {
			return nil
		}
		qualifies := false
		totalRep := math.LegacyZeroDec()
		add := func(scoreStr string) {
			score, perr := math.LegacyNewDecFromStr(scoreStr)
			if perr != nil {
				return
			}
			totalRep = totalRep.Add(score)
			if score.GTE(minReputation) {
				qualifies = true
			}
		}
		if tags != nil {
			for _, tag := range tags {
				if scoreStr, ok := member.ReputationScores[tag]; ok {
					add(scoreStr)
				}
			}
		} else {
			// Decimal addition is exact, so map order does not matter.
			for _, scoreStr := range member.ReputationScores {
				add(scoreStr)
			}
		}
		if qualifies {
			pool = append(pool, jurorCandidate{addr: addr, weight: k.candidateWeight(ctx, addr, totalRep)})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return pool, nil
}

// SelectJury draws up to jurySize jurors for review with the block-hash
// seed, excluding everyone juryExclusions names. The pool shrinks the jury to
// fit; an empty pool is an error ("insufficient eligible jurors"), which the
// escalation paths match on. On chain only DrawPendingJuries draws, through
// selectJuryWith.
func (k Keeper) SelectJury(ctx context.Context, review types.JuryReview, jurySize uint32) ([]string, error) {
	return k.selectJuryWith(ctx, review, jurySize, k.juryDrawRng(ctx, review.Id))
}

func (k Keeper) selectJuryWith(ctx context.Context, review types.JuryReview, jurySize uint32, rng *rand.Rand) ([]string, error) {
	pool, err := k.jurorPool(ctx, review)
	if err != nil {
		return nil, err
	}
	if len(pool) == 0 {
		return nil, fmt.Errorf("insufficient eligible jurors: need %d, have 0", jurySize)
	}
	return drawJurors(rng, pool, int(jurySize)), nil
}

// newPendingJuryDraw records which beacon epoch seeds reviewID's draw: the
// epoch after the one open now, when x/shield is wired and its beacon can
// produce it; seed_epoch 0 (block-hash seed, drawn in the next BeginBlocker)
// otherwise.
func (k Keeper) newPendingJuryDraw(ctx context.Context, reviewID uint64) types.PendingJuryDraw {
	draw := types.PendingJuryDraw{
		JuryReviewId:   reviewID,
		QueuedAtHeight: sdk.UnwrapSDKContext(ctx).BlockHeight(),
	}
	if k.late.beacon == nil {
		return draw
	}
	next := k.late.beacon.CurrentEpoch(ctx) + 1
	if k.late.beacon.BeaconStatus(ctx, next) != shieldtypes.BeaconStatus_BEACON_STATUS_UNAVAILABLE {
		draw.SeedEpoch = next
	}
	return draw
}

// queueJuryDraw asks the next BeginBlocker to draw seats more jurors onto review
// and records parties as never-drawable. The caller persists review. A
// review already queued keeps its seed epoch: the refill joins the draw
// already waiting.
func (k Keeper) queueJuryDraw(ctx context.Context, review *types.JuryReview, seats uint32, parties ...string) error {
	for _, p := range parties {
		if p != "" && !slices.Contains(review.ExcludedJurors, p) {
			review.ExcludedJurors = append(review.ExcludedJurors, p)
		}
	}
	if seats == 0 {
		return nil
	}
	review.PendingSeats += seats
	if _, err := k.PendingJuryDraws.Get(ctx, review.Id); err == nil {
		return nil
	} else if !errors.Is(err, collections.ErrNotFound) {
		return err
	}
	return k.PendingJuryDraws.Set(ctx, review.Id, k.newPendingJuryDraw(ctx, review.Id))
}

// openAppealJury records the parties of an appeal-style case (gov-action
// appeal, migration objection, external case) and queues its jury draw.
// Best-effort like before: a case nobody can be seated on still stands and
// resolves by committee or INCONCLUSIVE at its deadline.
func (k Keeper) openAppealJury(ctx context.Context, reviewID uint64, parties []string) error {
	params, err := k.Params.Get(ctx)
	if err != nil {
		return err
	}
	review, err := k.GetJuryReview(ctx, reviewID)
	if err != nil {
		return err
	}
	if err := k.queueJuryDraw(ctx, &review, params.JurySize, parties...); err != nil {
		return err
	}
	return k.JuryReview.Set(ctx, reviewID, review)
}

// juryDrawSeed decides how draw is seeded in this block: the beacon value
// of its seed epoch when AVAILABLE, the block hash when there is no beacon
// epoch, the beacon is UNAVAILABLE or the wait ran out (reason set), or not
// yet (ready false: the beacon is PENDING and the wait is still running).
func (k Keeper) juryDrawSeed(ctx context.Context, draw types.PendingJuryDraw) (rng *rand.Rand, source, fallbackReason string, ready bool) {
	if draw.SeedEpoch == 0 {
		return k.juryDrawRng(ctx, draw.JuryReviewId), seedSourceBlockHash, "", true
	}
	if k.late.beacon == nil {
		return k.juryDrawRng(ctx, draw.JuryReviewId), seedSourceBlockHash, fallbackUnavailable, true
	}
	switch k.late.beacon.BeaconStatus(ctx, draw.SeedEpoch) {
	case shieldtypes.BeaconStatus_BEACON_STATUS_AVAILABLE:
		if key, ok := k.late.beacon.BeaconKey(ctx, draw.SeedEpoch); ok {
			return juryBeaconRng(key, draw.JuryReviewId), seedSourceBeacon, "", true
		}
		return k.juryDrawRng(ctx, draw.JuryReviewId), seedSourceBlockHash, fallbackUnavailable, true
	case shieldtypes.BeaconStatus_BEACON_STATUS_PENDING:
		height := sdk.UnwrapSDKContext(ctx).BlockHeight()
		wait := types.JuryBeaconWaitBlocks(k.late.beacon.BeaconEpochInterval(ctx))
		if height-draw.QueuedAtHeight > wait {
			return k.juryDrawRng(ctx, draw.JuryReviewId), seedSourceBlockHash, fallbackWaitExceeded, true
		}
		return nil, "", "", false
	default:
		return k.juryDrawRng(ctx, draw.JuryReviewId), seedSourceBlockHash, fallbackUnavailable, true
	}
}

// DrawPendingJuries seats every review whose draw is ready. BeginBlocker
// only (see Keeper.BeginBlocker for why not the EndBlocker).
// A draw whose beacon value is still pending stays queued for a later block
// (bounded by JuryBeaconWaitBlocks, after which it falls back to the block
// hash). Each draw runs in its own cache context so one failing review never
// halts the chain or blocks the others; the queue entry is dropped either way
// (a failed draw leaves the review short, which the quorum floor and deadline
// paths already handle).
func (k Keeper) DrawPendingJuries(ctx context.Context) error {
	var draws []types.PendingJuryDraw
	if err := k.PendingJuryDraws.Walk(ctx, nil, func(_ uint64, draw types.PendingJuryDraw) (bool, error) {
		draws = append(draws, draw)
		return false, nil
	}); err != nil {
		return err
	}
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	var nextEpoch uint64
	if k.late.beacon != nil && len(draws) > 0 {
		nextEpoch = k.late.beacon.CurrentEpoch(ctx) + 1
	}
	for _, draw := range draws {
		id := draw.JuryReviewId
		if repointed, changed := k.repointFutureSeedEpoch(ctx, draw, nextEpoch); changed {
			draw = repointed
			if err := k.PendingJuryDraws.Set(ctx, id, draw); err != nil {
				return err
			}
		}
		rng, source, reason, ready := k.juryDrawSeed(ctx, draw)
		if !ready {
			continue
		}
		if reason != "" {
			sdkCtx.EventManager().EmitEvent(sdk.NewEvent(
				"jury_seed_fallback",
				sdk.NewAttribute("jury_review_id", fmt.Sprintf("%d", id)),
				sdk.NewAttribute("seed_epoch", fmt.Sprintf("%d", draw.SeedEpoch)),
				sdk.NewAttribute("reason", reason),
			))
		}
		cacheCtx, write := sdkCtx.CacheContext()
		if err := k.seatPendingJury(cacheCtx, id, rng, source); err != nil {
			sdkCtx.Logger().Error("failed to draw jury seats", "jury_review_id", id, "error", err)
		} else {
			write()
		}
		if err := k.PendingJuryDraws.Remove(ctx, id); err != nil {
			return err
		}
	}
	return nil
}

func (k Keeper) seatPendingJury(ctx context.Context, id uint64, rng *rand.Rand, seedSource string) error {
	review, err := k.JuryReview.Get(ctx, id)
	if err != nil {
		return err
	}
	if review.PendingSeats == 0 {
		return nil
	}
	seats := review.PendingSeats
	review.PendingSeats = 0
	if review.Verdict != types.Verdict_VERDICT_PENDING {
		return k.JuryReview.Set(ctx, id, review)
	}

	drawn, sErr := k.selectJuryWith(ctx, review, seats, rng)
	if sErr != nil {
		// An empty pool is not an error for the review: it proceeds with
		// whoever is seated, at the correspondingly lower quorum.
		drawn = nil
	}
	review.Jurors = append(review.Jurors, drawn...)
	for _, juror := range drawn {
		if err := k.JuryReviewsByJuror.Set(ctx, collections.Join(juror, review.Id)); err != nil {
			return err
		}
	}
	if len(drawn) > 0 {
		if err := k.RecordJurySeating(ctx, drawn); err != nil {
			return err
		}
	}
	if isAppealJury(review) {
		if len(review.Jurors) > 0 {
			review.RequiredVotes = commontypes.Saturate[uint32](k.appealRequiredVotes(ctx, len(review.Jurors)))
		}
	} else if err := k.recomputeRequiredVotes(ctx, &review); err != nil {
		return err
	}
	if len(drawn) > 0 {
		if err := k.restartAcceptanceWindow(ctx, &review); err != nil {
			return err
		}
	}
	if err := k.JuryReview.Set(ctx, id, review); err != nil {
		return err
	}

	// The seated addresses, so a juror's client can act on a plain event
	// filter instead of fetching every review to check whether it is theirs.
	sdk.UnwrapSDKContext(ctx).EventManager().EmitEvent(sdk.NewEvent(
		"jury_seated",
		sdk.NewAttribute("jury_review_id", fmt.Sprintf("%d", id)),
		sdk.NewAttribute("drawn", strings.Join(drawn, ",")),
		sdk.NewAttribute("jurors", strings.Join(review.Jurors, ",")),
		sdk.NewAttribute("seats_requested", fmt.Sprintf("%d", seats)),
		sdk.NewAttribute("seed_source", seedSource),
	))
	return nil
}

// restartAcceptanceWindow counts the acceptance window of an initiative jury
// from the block its seats were actually filled (REP-R-201). The window is
// first set when the review opens, but the draw that fills the seats runs in
// a later BeginBlocker and, with the beacon pending, up to JuryBeaconWaitBlocks
// later; a short window could elapse before anyone was seated, the sweep
// would find no silent juror and stand the clock down for good, and the
// jurors seated afterwards would never be swept for silence. A review that
// has used its redraws keeps the clock down (the sweep would skip it anyway).
// Clamped strictly inside the vote deadline like juryAcceptanceWindowBlocks.
func (k Keeper) restartAcceptanceWindow(ctx context.Context, review *types.JuryReview) error {
	if !isInitiativeJury(*review) || review.RedrawCount >= k.maxJuryRedraws(ctx) {
		return nil
	}
	params, err := k.Params.Get(ctx)
	if err != nil {
		return err
	}
	height := sdk.UnwrapSDKContext(ctx).BlockHeight()
	deadline := height + k.juryAcceptanceWindowBlocks(params)
	if review.Deadline > 0 && deadline >= review.Deadline {
		deadline = review.Deadline - 1
	}
	if deadline <= height {
		return nil
	}
	review.AcceptanceDeadline = deadline
	return nil
}

// clampImportedJuryDraw bounds the queued height of a pending draw imported
// from genesis to the import height (REP-R-206): a draw exported from a
// long-running chain would otherwise measure its fallback wait from a
// height the new chain has not reached. The seed epoch is clamped later, by
// repointFutureSeedEpoch in the first draw pass: x/rep's InitGenesis runs
// before x/shield's, so at import the beacon reads epoch 0 and UNAVAILABLE
// for every epoch, and clamping against it re-pointed every imported draw to
// the block-hash seed.
func (k Keeper) clampImportedJuryDraw(ctx context.Context, draw types.PendingJuryDraw) types.PendingJuryDraw {
	if height := sdk.UnwrapSDKContext(ctx).BlockHeight(); draw.QueuedAtHeight > height {
		draw.QueuedAtHeight = height
	}
	return draw
}

// repointFutureSeedEpoch re-points a draw whose seed epoch is past the next
// beacon epoch at the next epoch (or the block hash when that epoch yields
// no beacon), exactly as a draw queued now would be (REP-R-206). Only an
// imported draw can be that far ahead: newPendingJuryDraw always names the
// next epoch. Reports whether the draw changed.
// next is the beacon epoch after the current one, read once per pass.
func (k Keeper) repointFutureSeedEpoch(ctx context.Context, draw types.PendingJuryDraw, next uint64) (types.PendingJuryDraw, bool) {
	if draw.SeedEpoch == 0 || k.late.beacon == nil {
		return draw, false
	}
	if draw.SeedEpoch <= next {
		return draw, false
	}
	draw.SeedEpoch = next
	if k.late.beacon.BeaconStatus(ctx, next) == shieldtypes.BeaconStatus_BEACON_STATUS_UNAVAILABLE {
		draw.SeedEpoch = 0
	}
	return draw, true
}

package simulation

import (
	"cosmossdk.io/math"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/rep/types"
)

// Jury verdicts come from the seated sim jurors: they cast real
// MsgSubmitJurorVote txs, and the vote that reaches the required count
// triggers the tally in the handler.

// openJuries returns the PENDING jury reviews still inside their voting
// deadline with at least one seated live sim juror who has not voted.
func (e *simEnv) openJuries() ([]types.JuryReview, error) {
	height := e.ctx.BlockHeight()
	return e.juryReviews(func(rv types.JuryReview) bool {
		if rv.Verdict != types.Verdict_VERDICT_PENDING || height > rv.Deadline {
			return false
		}
		return len(e.unvotedJurors(rv)) > 0
	})
}

// unvotedJurors returns the live sim accounts seated on rv that have not
// voted yet.
func (e *simEnv) unvotedJurors(rv types.JuryReview) []simtypes.Account {
	cast := map[string]bool{}
	for _, v := range rv.Votes {
		if v != nil {
			cast[v.Juror] = true
		}
	}
	var out []simtypes.Account
	for _, j := range rv.Jurors {
		if cast[j] {
			continue
		}
		if acc, ok := e.account(j); ok && e.spendable(acc).IsPositive() {
			out = append(out, acc)
		}
	}
	return out
}

// jurorVote builds a juror's vote on a review for side.
func (e *simEnv) jurorVote(acc simtypes.Account, reviewID uint64, side types.Verdict) *types.MsgSubmitJurorVote {
	confidence := math.LegacyNewDecWithPrec(int64(50+e.r.Intn(51)), 2)
	return &types.MsgSubmitJurorVote{
		Juror:        acc.Address.String(),
		JuryReviewId: reviewID,
		Verdict:      side,
		Confidence:   &confidence,
		Reasoning:    randomReason(e.r),
	}
}

// randomSide returns a juror verdict, uphold or reject.
func (e *simEnv) randomSide() types.Verdict {
	if e.r.Intn(2) == 0 {
		return types.Verdict_VERDICT_REJECT_CHALLENGE
	}
	return types.Verdict_VERDICT_UPHOLD_CHALLENGE
}

// voteJury has the seated live sim jurors of rv that have not voted cast a
// vote for one side (agreeing jurors reach a supermajority; votes split
// across calls end INCONCLUSIVE), until the review leaves PENDING. It
// returns the last vote delivered (OK false when none was).
func (e *simEnv) voteJury(rv types.JuryReview) (simtypes.OperationMsg, error) {
	side := e.randomSide()
	if e.isObjectionReview(rv.Id) {
		side = types.Verdict_VERDICT_UPHOLD_CHALLENGE
	}
	var last simtypes.OperationMsg
	for _, acc := range e.unvotedJurors(rv) {
		msg := e.jurorVote(acc, rv.Id, side)
		if e.dryRun(msg) != nil {
			continue
		}
		opMsg, err := e.deliverSmallFee(acc, msg)
		if err != nil {
			return opMsg, err
		}
		if opMsg.OK {
			last = opMsg
		}
		fresh, err := e.k.JuryReview.Get(e.ctx, rv.Id)
		if err != nil || fresh.Verdict != types.Verdict_VERDICT_PENDING {
			break
		}
	}
	return last, nil
}

// isObjectionReview reports whether review id decides an objection to a
// council migration. Its jurors always uphold the objection: rejecting it,
// or a jury that cannot decide, finalizes the migration and tombstones a
// sim account (migration_helpers.go).
func (e *simEnv) isObjectionReview(id uint64) bool {
	ms, err := e.migrations(func(m types.MemberMigration) bool {
		return m.JuryReviewId == id && m.Status == types.MigrationStatus_MIGRATION_STATUS_OBJECTED
	})
	return err == nil && len(ms) > 0
}

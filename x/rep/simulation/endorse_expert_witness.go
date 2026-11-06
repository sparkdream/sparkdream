package simulation

import (
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

// SimulateMsgEndorseExpertWitness has a second seated sim juror endorse a
// pending expert invitation on a pending initiative jury review. The
// candidate list skips the inviter and already-endorsed invitations; the
// handler decides whether the juror has answered the summons (firstValid
// dry-runs each pairing).
func SimulateMsgEndorseExpertWitness(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	label := &types.MsgEndorseExpertWitness{}
	return operation(ak, bk, k, txGen, label, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		height := e.ctx.BlockHeight()
		rvs, err := e.juryReviews(func(rv types.JuryReview) bool {
			if rv.Verdict != types.Verdict_VERDICT_PENDING || height >= rv.Deadline {
				return false
			}
			for i := range rv.ExpertWitnesses {
				if i >= len(rv.ExpertEndorsers) || rv.ExpertEndorsers[i] == "" {
					return true
				}
			}
			return false
		})
		if err != nil {
			return noOpErr(label, err)
		}
		var cands []candidate
		for _, rv := range rvs {
			for i, expert := range rv.ExpertWitnesses {
				if i < len(rv.ExpertEndorsers) && rv.ExpertEndorsers[i] != "" {
					continue
				}
				inviter := ""
				if i < len(rv.ExpertInviters) {
					inviter = rv.ExpertInviters[i]
				}
				for _, j := range rv.Jurors {
					if j == inviter {
						continue
					}
					acc, ok := e.account(j)
					if !ok {
						continue
					}
					cands = append(cands, candidate{acc: acc, msg: &types.MsgEndorseExpertWitness{
						Juror: j, JuryReviewId: rv.Id, Expert: expert,
					}, spent: math.ZeroInt()})
				}
			}
		}
		return e.firstValid(label, cands, 6, "no second sim juror to endorse a pending expert invitation")
	})
}

package simulation

import (
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

// SimulateMsgSubmitExpertTestimony has an expert witness of a PENDING jury
// review testify. Experts are seated by SimulateMsgInviteExpertWitness.
func SimulateMsgSubmitExpertTestimony(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	label := &types.MsgSubmitExpertTestimony{}
	return operation(ak, bk, k, txGen, label, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		rvs, err := e.juryReviews(func(rv types.JuryReview) bool {
			return rv.Verdict == types.Verdict_VERDICT_PENDING && len(rv.ExpertWitnesses) > 0
		})
		if err != nil {
			return noOpErr(label, err)
		}
		var cands []candidate
		for _, rv := range rvs {
			for _, w := range rv.ExpertWitnesses {
				if acc, ok := e.account(w); ok {
					cands = append(cands, candidate{acc: acc, msg: &types.MsgSubmitExpertTestimony{
						Expert: w, JuryReviewId: rv.Id, Opinion: randomReason(e.r), Reasoning: randomReason(e.r),
					}, spent: math.ZeroInt()})
				}
			}
		}
		return e.firstValid(label, cands, 4, "no pending jury review has an expert witness yet")
	})
}

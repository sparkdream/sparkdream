package simulation

import (
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

// SimulateMsgSubmitJurorVote has the seated sim jurors of a PENDING jury
// review (an initiative challenge, a content challenge, a moderation appeal
// or a migration objection) that have not voted cast their votes, all for
// one side, until the review is tallied.
func SimulateMsgSubmitJurorVote(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	label := &types.MsgSubmitJurorVote{}
	return operation(ak, bk, k, txGen, label, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		rvs, err := e.openJuries()
		if err != nil {
			return noOpErr(label, err)
		}
		for _, i := range e.r.Perm(len(rvs)) {
			opMsg, err := e.voteJury(rvs[i])
			if err != nil || opMsg.OK {
				return opMsg, nil, err
			}
		}
		return noOp(label, "no PENDING jury review with a seated live sim juror left to vote")
	})
}

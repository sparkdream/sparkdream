package simulation

import (
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/commons/keeper"
	"sparkdream/x/commons/types"
)

// SimulateMsgCancelPoll withdraws an open poll nobody has voted on yet,
// signed by its creator (a live sim account).
func SimulateMsgCancelPoll(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	label := &types.MsgCancelPoll{}
	return operation(ak, bk, k, txGen, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		polls, err := e.openPolls(func(p types.Poll) bool {
			for _, t := range p.Tallies {
				if t > 0 {
					return false
				}
			}
			_, ok := e.account(p.Creator)
			return ok
		})
		if err != nil {
			return noOpErr(label, err)
		}
		p, ok := pick(e.r, polls)
		if !ok {
			return noOp(label, "no vote-free open poll by a live sim account")
		}
		creator, _ := e.account(p.Creator)
		return e.submit(creator, &types.MsgCancelPoll{Creator: p.Creator, PollId: p.Id})
	})
}

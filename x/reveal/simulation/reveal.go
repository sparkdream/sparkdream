package simulation

import (
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/reveal/keeper"
	"sparkdream/x/reveal/types"
)

// SimulateMsgReveal has a contributor reveal the code of a BACKED tranche.
// When none is BACKED it first fills a STAKING tranche with real stakes.
func SimulateMsgReveal(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	return operation(ak, bk, k, txGen, &types.MsgReveal{}, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		refs, err := e.backedTranches()
		if err != nil {
			return noOpErr(&types.MsgReveal{}, err)
		}
		ref, ok := pick(e.r, refs)
		if !ok {
			if ref, ok, err = e.ensureBacked(); err != nil {
				return noOpErr(&types.MsgReveal{}, err)
			}
			if !ok {
				return noOp(&types.MsgReveal{}, "no BACKED tranche and none could be backed")
			}
		}
		acc, msg, ok := e.revealMsgFor(ref)
		if !ok {
			return noOp(&types.MsgReveal{}, "contributor is not a live sim account")
		}
		return e.submit(acc, msg)
	})
}

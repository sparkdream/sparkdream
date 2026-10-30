package simulation

import (
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/reveal/keeper"
	"sparkdream/x/reveal/types"
)

// SimulateMsgStake stakes DREAM on a STAKING tranche from a member other
// than the contributor, never past the tranche's threshold. When no tranche
// is open for staking it first advances a contribution's approval through
// the Commons Council (council.go).
func SimulateMsgStake(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	return operation(ak, bk, k, txGen, &types.MsgStake{}, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		refs, err := e.ensureStaking()
		if err != nil {
			return noOpErr(&types.MsgStake{}, err)
		}
		ref, ok := pick(e.r, refs)
		if !ok {
			return noOp(&types.MsgStake{}, "no STAKING tranche (approval still in council)")
		}
		acc, msg, ok := e.stakeMsgFor(ref)
		if !ok {
			return noOp(&types.MsgStake{}, "no member can stake on the tranche")
		}
		return e.submit(acc, msg)
	})
}

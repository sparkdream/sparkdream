package simulation

import (
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/reveal/keeper"
	"sparkdream/x/reveal/types"
)

// SimulateMsgWithdraw withdraws a stake from a tranche that is still
// STAKING, signed by the staker. Stakes on BACKED, REVEALED or DISPUTED
// tranches stay committed (spec, Withdrawal Rules) and terminal tranches
// return them automatically, so those are never picked.
func SimulateMsgWithdraw(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	return operation(ak, bk, k, txGen, &types.MsgWithdraw{}, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		refs, err := e.tranchesIn(types.TrancheStatus_TRANCHE_STATUS_STAKING, func(trancheRef) bool { return true })
		if err != nil {
			return noOpErr(&types.MsgWithdraw{}, err)
		}
		type target struct {
			acc   simtypes.Account
			stake types.RevealStake
		}
		var targets []target
		for _, ref := range refs {
			stakes, err := e.trancheStakes(ref.c.Id, ref.t.Id)
			if err != nil {
				return noOpErr(&types.MsgWithdraw{}, err)
			}
			for _, s := range stakes {
				if acc, ok := e.account(s.Staker); ok {
					targets = append(targets, target{acc: acc, stake: s})
				}
			}
		}
		tg, ok := pick(e.r, targets)
		if !ok {
			return noOp(&types.MsgWithdraw{}, "no stake on a STAKING tranche")
		}
		return e.submit(tg.acc, &types.MsgWithdraw{Staker: tg.acc.Address.String(), StakeId: tg.stake.Id})
	})
}

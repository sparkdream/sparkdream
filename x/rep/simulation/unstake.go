package simulation

import (
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

// SimulateMsgUnstake has a staker withdraw all or part of one of its
// initiative, project, member or tag stakes.
func SimulateMsgUnstake(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	label := &types.MsgUnstake{}
	return operation(ak, bk, k, txGen, label, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		stakes, err := e.stakes(func(s types.Stake) bool {
			if s.TargetType > types.StakeTargetType_STAKE_TARGET_TAG {
				return false // content stakes and author bonds belong to their modules
			}
			_, ok := e.account(s.Staker)
			return ok
		})
		if err != nil {
			return noOpErr(label, err)
		}
		cands := make([]candidate, 0, len(stakes))
		for _, s := range stakes {
			acc, _ := e.account(s.Staker)
			msg := &types.MsgUnstake{Staker: s.Staker, StakeId: s.Id}
			if e.r.Intn(2) == 0 && s.Amount.GT(math.OneInt()) {
				msg.Amount = PtrInt(randBetween(e.r, math.OneInt(), s.Amount.QuoRaw(2)))
			}
			cands = append(cands, candidate{acc: acc, msg: msg, spent: math.ZeroInt()})
		}
		return e.firstValid(label, cands, 4, "no stake held by a live sim account")
	})
}

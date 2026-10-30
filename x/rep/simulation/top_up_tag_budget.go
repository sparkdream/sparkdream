package simulation

import (
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

// SimulateMsgTopUpTagBudget has a member of a budget's group add SPARK to it.
func SimulateMsgTopUpTagBudget(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	label := &types.MsgTopUpTagBudget{}
	return operation(ak, bk, k, txGen, label, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		bs, err := e.tagBudgets(nil)
		if err != nil {
			return noOpErr(label, err)
		}
		var cands []candidate
		for _, b := range bs {
			g, ok := e.groupByPolicy(b.GroupAccount)
			if !ok {
				continue
			}
			by, ok := pick(e.r, e.liveSeats(g.Index))
			if !ok {
				continue
			}
			amt := math.NewInt(int64(1+e.r.Intn(20)) * 100_000) // 0.1-2 SPARK
			if e.spendable(by).LTE(amt.MulRaw(3)) {
				continue
			}
			cands = append(cands, candidate{acc: by, msg: &types.MsgTopUpTagBudget{
				Creator: by.Address.String(), BudgetId: b.Id, Amount: amt.String(),
			}, spent: amt})
		}
		return e.firstValid(label, cands, 4, "no tag budget with a live group member who can top it up")
	})
}

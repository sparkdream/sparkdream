package simulation

import (
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

// SimulateMsgWithdrawTagBudget returns a tag budget's remaining pool to its
// group and deactivates it, through an executed x/commons proposal of the
// group (council.go).
func SimulateMsgWithdrawTagBudget(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	label := &types.MsgWithdrawTagBudget{}
	return operation(ak, bk, k, txGen, label, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		bodies := e.groupsAllowing(sdk.MsgTypeURL(label))
		if len(bodies) == 0 {
			return noOp(label, "no x/commons group may execute MsgWithdrawTagBudget")
		}
		return e.advance(label, bodies, decoder(func() *types.MsgWithdrawTagBudget { return &types.MsgWithdrawTagBudget{} }),
			func(policy string, taken []sdk.Msg) (sdk.Msg, string, error) {
				covered := takenIDs(taken, func(m *types.MsgWithdrawTagBudget) uint64 { return m.BudgetId })
				bs, err := e.tagBudgets(func(b types.TagBudget) bool {
					return b.GroupAccount == policy && !covered[b.Id] && poolBalance(b).IsPositive()
				})
				if err != nil {
					return nil, "", err
				}
				for _, i := range e.r.Perm(len(bs)) {
					msg := &types.MsgWithdrawTagBudget{Creator: policy, BudgetId: bs[i].Id}
					if e.dryRun(msg) == nil {
						return msg, "", nil
					}
				}
				return nil, "the body holds no funded tag budget", nil
			})
	})
}

package simulation

import (
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

// SimulateMsgToggleTagBudget pauses or resumes a tag budget through an
// executed x/commons proposal of the budget's group (council.go). A paused
// budget is resumed only while its group holds no other active budget for
// the tag.
func SimulateMsgToggleTagBudget(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	label := &types.MsgToggleTagBudget{}
	return operation(ak, bk, k, txGen, label, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		bodies := e.groupsAllowing(sdk.MsgTypeURL(label))
		if len(bodies) == 0 {
			return noOp(label, "no x/commons group may execute MsgToggleTagBudget")
		}
		return e.advance(label, bodies, decoder(func() *types.MsgToggleTagBudget { return &types.MsgToggleTagBudget{} }),
			func(policy string, taken []sdk.Msg) (sdk.Msg, string, error) {
				covered := takenIDs(taken, func(m *types.MsgToggleTagBudget) uint64 { return m.BudgetId })
				bs, err := e.tagBudgets(func(b types.TagBudget) bool { return b.GroupAccount == policy && !covered[b.Id] })
				if err != nil {
					return nil, "", err
				}
				activeTag := map[string]bool{}
				for _, b := range bs {
					if b.Active {
						activeTag[b.Tag] = true
					}
				}
				for _, i := range e.r.Perm(len(bs)) {
					b := bs[i]
					if !b.Active && (activeTag[b.Tag] || poolBalance(b).IsZero()) {
						continue
					}
					msg := &types.MsgToggleTagBudget{Creator: policy, BudgetId: b.Id, Active: !b.Active}
					if e.dryRun(msg) == nil {
						return msg, "", nil
					}
				}
				return nil, "the body holds no tag budget to toggle", nil
			})
	})
}

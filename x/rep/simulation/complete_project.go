package simulation

import (
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

// SimulateMsgCompleteProject completes an ACTIVE project through an executed
// x/commons proposal of its council or Operations Committee (council.go),
// one step per call.
func SimulateMsgCompleteProject(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	label := &types.MsgCompleteProject{}
	return operation(ak, bk, k, txGen, label, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		bodies := e.groupsAllowing(sdk.MsgTypeURL(label))
		if len(bodies) == 0 {
			return noOp(label, "no x/commons group may execute MsgCompleteProject")
		}
		return e.advance(label, bodies, decoder(func() *types.MsgCompleteProject { return &types.MsgCompleteProject{} }),
			func(policy string, taken []sdk.Msg) (sdk.Msg, string, error) {
				covered := takenIDs(taken, func(m *types.MsgCompleteProject) uint64 { return m.ProjectId })
				ps, err := e.activeProjects()
				if err != nil {
					return nil, "", err
				}
				for _, i := range e.r.Perm(len(ps)) {
					if covered[ps[i].Id] {
						continue
					}
					msg := &types.MsgCompleteProject{Creator: policy, ProjectId: ps[i].Id}
					if e.dryRun(msg) == nil {
						return msg, "", nil
					}
				}
				return nil, "no ACTIVE project this body may complete", nil
			})
	})
}

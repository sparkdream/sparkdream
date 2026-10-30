package simulation

import (
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

// SimulateMsgApproveInterim approves (or now and then rejects) an open
// interim through an executed x/commons proposal of the Commons Operations
// Committee (council.go), one step per call.
func SimulateMsgApproveInterim(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	label := &types.MsgApproveInterim{}
	return operation(ak, bk, k, txGen, label, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		bodies := e.groupsAllowing(sdk.MsgTypeURL(label))
		if len(bodies) == 0 {
			return noOp(label, "no x/commons group may execute MsgApproveInterim")
		}
		return e.advance(label, bodies, decoder(func() *types.MsgApproveInterim { return &types.MsgApproveInterim{} }),
			func(policy string, taken []sdk.Msg) (sdk.Msg, string, error) {
				covered := takenIDs(taken, func(m *types.MsgApproveInterim) uint64 { return m.InterimId })
				its, err := e.openInterim(func(it types.Interim) bool { return !covered[it.Id] })
				if err != nil {
					return nil, "", err
				}
				for _, i := range e.r.Perm(len(its)) {
					msg := &types.MsgApproveInterim{
						Creator: policy, InterimId: its[i].Id, Approved: e.r.Intn(6) != 0, Comments: "simulation review",
					}
					if e.dryRun(msg) == nil {
						return msg, "", nil
					}
				}
				return nil, "no open interim this body may approve", nil
			})
	})
}

// takenIDs collects the ids carried by the open proposals' messages of type T.
func takenIDs[T sdk.Msg](taken []sdk.Msg, id func(T) uint64) map[uint64]bool {
	out := map[uint64]bool{}
	for _, m := range taken {
		if t, ok := m.(T); ok {
			out[id(t)] = true
		}
	}
	return out
}

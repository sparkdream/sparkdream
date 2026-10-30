package simulation

import (
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

// SimulateMsgAssignInterim has an Operations Committee seat add a member to
// a PENDING interim. With none, a member opens one first.
func SimulateMsgAssignInterim(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	label := &types.MsgAssignInterim{}
	return operation(ak, bk, k, txGen, label, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		ops := append(e.liveSeats(commonsOpsCommittee), e.liveSeats(technicalOpsCommittee)...)
		if len(ops) == 0 {
			return noOp(label, "no live Operations Committee seat")
		}
		its, err := e.openInterim(func(it types.Interim) bool { return it.Status == types.InterimStatus_INTERIM_STATUS_PENDING })
		if err != nil {
			return noOpErr(label, err)
		}
		ms, err := e.members(nil)
		if err != nil {
			return noOpErr(label, err)
		}
		var cands []candidate
		for _, it := range its {
			by := ops[e.r.Intn(len(ops))]
			to, ok := pick(e.r, ms)
			if !ok {
				break
			}
			cands = append(cands, candidate{acc: by, msg: &types.MsgAssignInterim{
				Creator: by.Address.String(), InterimId: it.Id, Assignee: to.addr(),
			}, spent: math.ZeroInt()})
		}
		return e.firstValid(label, cands, 4, "no PENDING interim to assign")
	})
}

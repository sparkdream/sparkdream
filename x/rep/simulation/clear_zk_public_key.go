package simulation

import (
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

// SimulateMsgClearZkPublicKey has a member holding an active or pending ZK
// key clear it (a leaked key).
func SimulateMsgClearZkPublicKey(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	label := &types.MsgClearZkPublicKey{}
	return operation(ak, bk, k, txGen, label, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		ms, err := e.members(func(ma memberAcc) bool {
			return len(ma.m.ZkPublicKey) > 0 || len(ma.m.PendingZkPublicKey) > 0
		})
		if err != nil {
			return noOpErr(label, err)
		}
		cands := make([]candidate, 0, len(ms))
		for _, ma := range ms {
			cands = append(cands, candidate{acc: ma.acc, msg: &types.MsgClearZkPublicKey{Member: ma.addr()}, spent: math.ZeroInt()})
		}
		return e.firstValid(label, cands, 4, "no member holds a ZK key")
	})
}

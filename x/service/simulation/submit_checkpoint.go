package simulation

import (
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/service/keeper"
	"sparkdream/x/service/types"
)

// SimulateMsgSubmitCheckpoint has an ACTIVE or UNDERFUNDED operator record
// how far its off-chain work has progressed: a height above its previous
// checkpoint, at most the current height, and a 32-byte root. When no sim
// account is such an operator one registers first.
func SimulateMsgSubmitCheckpoint(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	label := &types.MsgSubmitCheckpoint{}
	return operation(ak, bk, k, txGen, label, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		height := e.ctx.BlockHeight()
		prev := func(op types.Operator) int64 {
			acc, err := e.operatorBytes(op)
			if err != nil {
				return height
			}
			if cp, ok := e.k.GetCheckpoint(e.ctx, acc, op.ServiceType); ok {
				return cp.Height
			}
			return 0
		}
		op, acc, ok, err := e.signedOperator(func(op types.Operator) bool {
			return (op.Status == types.OperatorStatus_OPERATOR_STATUS_ACTIVE ||
				op.Status == types.OperatorStatus_OPERATOR_STATUS_UNDERFUNDED) && prev(op) < height
		})
		if err != nil {
			return noOpErr(label, err)
		}
		if !ok {
			return noOp(label, "no ACTIVE or UNDERFUNDED operator behind the chain signed by a live sim account")
		}
		last := prev(op)
		if last >= height {
			return noOp(label, "operator's checkpoint is already at the current height")
		}
		msg := &types.MsgSubmitCheckpoint{
			Operator:    op.Address,
			ServiceType: op.ServiceType,
			Height:      last + 1 + e.r.Int63n(height-last),
			Root:        randomRoot(e.r),
		}
		return e.submit(acc, msg, math.ZeroInt())
	})
}

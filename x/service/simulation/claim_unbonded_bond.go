package simulation

import (
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/service/keeper"
	"sparkdream/x/service/types"
)

// SimulateMsgClaimUnbondedBond claims a matured bond: the whole bond of an
// UNBONDING operator past unbond_complete_at (retiring it), or the queued
// partial withdrawal of an ACTIVE operator past withdraw_complete_at. Both
// wait out a period, so there is no prerequisite to deliver; the handler's
// open-report and tier-1 escrow gates are left to the dry-run.
func SimulateMsgClaimUnbondedBond(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	label := &types.MsgClaimUnbondedBond{}
	return operation(ak, bk, k, txGen, label, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		height := e.ctx.BlockHeight()
		ops, err := e.signedOperators(func(op types.Operator) bool {
			switch op.Status {
			case types.OperatorStatus_OPERATOR_STATUS_UNBONDING:
				return height >= op.UnbondCompleteAt
			case types.OperatorStatus_OPERATOR_STATUS_ACTIVE:
				return pendingWithdraw(op).IsPositive() && height >= op.WithdrawCompleteAt
			default:
				return false
			}
		})
		if err != nil {
			return noOpErr(label, err)
		}
		if len(ops) == 0 {
			return noOp(label, "no matured exit or partial withdrawal")
		}
		reason := ""
		for _, i := range e.r.Perm(len(ops)) {
			op := ops[i]
			acc, _ := e.account(op.Address)
			msg := &types.MsgClaimUnbondedBond{Operator: op.Address, ServiceType: op.ServiceType}
			if why := e.usable(acc, math.ZeroInt()); why != "" {
				reason = why
				continue
			}
			if err := e.dryRun(msg); err != nil {
				reason = "handler would reject: " + err.Error()
				continue
			}
			return e.deliver(acc, msg, math.ZeroInt())
		}
		return noOp(label, reason)
	})
}

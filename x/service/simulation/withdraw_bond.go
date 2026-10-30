package simulation

import (
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/service/keeper"
	"sparkdream/x/service/types"
)

// SimulateMsgWithdrawBond queues a partial withdrawal on an ACTIVE operator
// with bond to spare above min_bond and its already queued withdrawal. When
// no sim account is such an operator one registers first (registration
// bonds up to twice min_bond, so a new operator usually has spare bond).
func SimulateMsgWithdrawBond(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	label := &types.MsgWithdrawBond{}
	return operation(ak, bk, k, txGen, label, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		var storeErr error
		spare := func(op types.Operator) math.Int {
			cfg, found, err := e.serviceType(op.ServiceType)
			if err != nil {
				storeErr = err
			}
			if !found {
				return math.ZeroInt()
			}
			return op.BondAmount.Sub(cfg.MinBondAmount).Sub(pendingWithdraw(op))
		}
		op, acc, ok, err := e.signedOperator(func(op types.Operator) bool {
			return op.Status == types.OperatorStatus_OPERATOR_STATUS_ACTIVE && spare(op).IsPositive()
		})
		if err == nil {
			err = storeErr
		}
		if err != nil {
			return noOpErr(label, err)
		}
		if !ok {
			return noOp(label, "no ACTIVE operator with bond above min_bond signed by a live sim account")
		}
		msg := &types.MsgWithdrawBond{
			Operator:    op.Address,
			ServiceType: op.ServiceType,
			Amount:      simtypes.RandomAmount(e.r, spare(op)),
		}
		if !msg.Amount.IsPositive() {
			msg.Amount = math.OneInt()
		}
		return e.submit(acc, msg, math.ZeroInt())
	})
}

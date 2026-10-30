package simulation

import (
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/service/keeper"
	"sparkdream/x/service/types"
)

// SimulateMsgTopUpBond adds SPARK to the bond of an ACTIVE or UNDERFUNDED
// operator. An UNDERFUNDED operator is topped up to at least min_bond half
// the time (returning it to ACTIVE). When no sim account is an operator one
// registers first.
func SimulateMsgTopUpBond(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	label := &types.MsgTopUpBond{}
	return operation(ak, bk, k, txGen, label, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		op, acc, ok, err := e.signedOperator(func(op types.Operator) bool {
			return op.Status == types.OperatorStatus_OPERATOR_STATUS_ACTIVE ||
				op.Status == types.OperatorStatus_OPERATOR_STATUS_UNDERFUNDED
		})
		if err != nil {
			return noOpErr(label, err)
		}
		if !ok {
			return noOp(label, "no ACTIVE or UNDERFUNDED operator signed by a live sim account")
		}
		// Up to 2 SPARK, or exactly what returns an UNDERFUNDED operator to
		// min_bond.
		amount := math.NewInt(1 + e.r.Int63n(2_000_000))
		if op.Status == types.OperatorStatus_OPERATOR_STATUS_UNDERFUNDED && e.r.Intn(2) == 0 {
			cfg, found, err := e.serviceType(op.ServiceType)
			if err != nil {
				return noOpErr(label, err)
			}
			if found && cfg.MinBondAmount.GT(op.BondAmount) {
				amount = cfg.MinBondAmount.Sub(op.BondAmount)
			}
		}
		if e.spendable(acc).LTE(amount) {
			amount = e.spendable(acc).QuoRaw(10)
		}
		if !amount.IsPositive() {
			return noOp(label, "operator holds no SPARK to add")
		}
		msg := &types.MsgTopUpBond{
			Operator:             op.Address,
			ServiceType:          op.ServiceType,
			AdditionalBondAmount: amount,
		}
		return e.submit(acc, msg, amount)
	})
}

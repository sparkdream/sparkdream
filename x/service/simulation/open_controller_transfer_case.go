package simulation

import (
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/service/keeper"
	"sparkdream/x/service/types"
)

// SimulateMsgOpenControllerTransferCase opens a jury case proposing another
// x/commons group as an operator's controller. The opener is the operator
// itself or a member meeting min_reporter_trust_level, and pays the
// report deposit. When there is no operator without an open case one
// registers first.
func SimulateMsgOpenControllerTransferCase(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	label := &types.MsgOpenControllerTransferCase{}
	return operation(ak, bk, k, txGen, label, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		cases, err := e.transferCases()
		if err != nil {
			return noOpErr(label, err)
		}
		open := map[string]bool{}
		for _, c := range cases {
			open[c.OperatorAddress+"/"+c.ServiceType] = true
		}
		ops, err := e.operators(func(op types.Operator) bool { return !open[op.Address+"/"+op.ServiceType] })
		if err != nil {
			return noOpErr(label, err)
		}
		if len(ops) == 0 {
			op, ok, err := e.registerOperator("")
			if err != nil {
				return noOpErr(label, err)
			}
			if !ok {
				return noOp(label, "no operator without an open case, and none could register")
			}
			ops = append(ops, op)
		}
		op, _ := pick(e.r, ops)

		var targets []string
		for _, g := range e.allGroups() {
			if g.PolicyAddress != op.Controller && len(e.groupMembers(g.Index)) > 0 {
				targets = append(targets, g.PolicyAddress)
			}
		}
		target, ok := pick(e.r, targets)
		if !ok {
			return noOp(label, "no other x/commons group with members to propose")
		}

		deposit := e.params.ReportDepositAmount
		var openers []simtypes.Account
		if acc, ok := e.account(op.Address); ok && e.r.Intn(2) == 0 {
			openers = append(openers, acc)
		}
		for _, i := range e.r.Perm(len(e.accs)) {
			if len(openers) >= 5 {
				break
			}
			acc := e.accs[i]
			if acc.Address.String() != op.Address && e.meetsReporterTrust(acc.Address.String()) {
				openers = append(openers, acc)
			}
		}
		reason := "no sim account may open the case"
		for _, acc := range openers {
			msg := &types.MsgOpenControllerTransferCase{
				Opener:                acc.Address.String(),
				Operator:              op.Address,
				ServiceType:           op.ServiceType,
				ProposedNewController: target,
				Reason:                randomReason(e.r),
			}
			if why := e.usable(acc, deposit); why != "" {
				reason = why
				continue
			}
			if err := e.dryRun(msg); err != nil {
				reason = "handler would reject: " + err.Error()
				continue
			}
			return e.deliver(acc, msg, deposit)
		}
		return noOp(label, reason)
	})
}

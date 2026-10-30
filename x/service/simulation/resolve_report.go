package simulation

import (
	"github.com/cosmos/cosmos-sdk/client"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/service/keeper"
	"sparkdream/x/service/types"
)

// SimulateMsgResolveReport resolves a PENDING report through an executed
// x/commons proposal of the operator's controller group (council.go), one
// step per call. The verdict is a dismissal, a tier-1 slash within the
// service type's unilateral cap, or an escalation to an x/rep jury with a
// proposed slash. When the controller has no pending report a reporter
// files one first (a real MsgReportOperator), and when it controls no
// operator one registers under it.
func SimulateMsgResolveReport(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	label := &types.MsgResolveReport{}
	return operation(ak, bk, k, txGen, label, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		// Any group may control an operator, but only a group whose
		// allowlist carries MsgResolveReport can execute one.
		bodies := e.groupsAllowing(sdk.MsgTypeURL(label))
		if len(bodies) == 0 {
			return noOp(label, "no x/commons group may execute MsgResolveReport")
		}
		return e.advance(label, bodies, decodeResolveReport, e.buildResolveReport)
	})
}

// decodeResolveReport decodes a proposal message carrying a
// MsgResolveReport.
func decodeResolveReport(a *codectypes.Any) (sdk.Msg, bool) {
	m := &types.MsgResolveReport{}
	if a == nil || m.Unmarshal(a.Value) != nil {
		return nil, false
	}
	return m, true
}

// buildResolveReport builds a MsgResolveReport signed by policy for a
// PENDING report against an operator policy controls that no open proposal
// already resolves.
func (e *simEnv) buildResolveReport(policy string, taken []sdk.Msg) (sdk.Msg, string, error) {
	covered := map[uint64]bool{}
	for _, m := range taken {
		if rr, ok := m.(*types.MsgResolveReport); ok {
			covered[rr.ReportId] = true
		}
	}
	controls := func(addr, serviceType string) bool {
		op, ok := e.operator(addr, serviceType)
		return ok && op.Controller == policy
	}
	pending, err := e.reports(types.ReportStatus_REPORT_STATUS_PENDING, func(r types.Report) bool {
		return !covered[r.ReportId] && controls(r.OperatorAddress, r.ServiceType)
	})
	if err != nil {
		return nil, "", err
	}
	report, ok := pick(e.r, pending)
	if !ok {
		var why string
		report, why, err = e.fileReportUnder(policy)
		if err != nil || why != "" {
			return nil, "no pending report: " + why, err
		}
	}
	op, ok := e.operator(report.OperatorAddress, report.ServiceType)
	if !ok {
		return nil, "the reported operator is gone", nil
	}
	cfg, found, err := e.serviceType(op.ServiceType)
	if err != nil || !found {
		return nil, "the operator's service type is gone", err
	}

	dismiss := &types.MsgResolveReport{Controller: policy, ReportId: report.ReportId, Verdict: types.ResolveVerdict_RESOLVE_VERDICT_T1_DISMISS}
	candidates := []sdk.Msg{dismiss}
	if capBps := cfg.UnilateralSlashCapBps; capBps > 0 {
		slash := &types.MsgResolveReport{
			Controller: policy,
			ReportId:   report.ReportId,
			Verdict:    types.ResolveVerdict_RESOLVE_VERDICT_T1_SLASH,
			SlashBps:   1 + e.r.Uint32()%capBps,
		}
		escalate := &types.MsgResolveReport{
			Controller: policy,
			ReportId:   report.ReportId,
			Verdict:    types.ResolveVerdict_RESOLVE_VERDICT_ESCALATE_TO_JURY,
			SlashBps:   1 + e.r.Uint32()%(2*capBps),
		}
		// Half slash (contestable by the operator), a quarter escalate, a
		// quarter dismiss.
		switch e.r.Intn(4) {
		case 0, 1:
			candidates = []sdk.Msg{slash, escalate, dismiss}
		case 2:
			candidates = []sdk.Msg{escalate, slash, dismiss}
		}
	}
	// The first verdict the handler accepts (a slash can hit the tier-1
	// cooldown or aggregate cap; a dismissal always applies).
	for _, msg := range candidates {
		if e.dryRun(msg) == nil {
			return msg, "", nil
		}
	}
	return nil, "the handler accepts no verdict on report", nil
}

// fileReportUnder files a report (a real MsgReportOperator) against an
// operator policy controls, registering one under policy first when it
// controls none.
func (e *simEnv) fileReportUnder(policy string) (types.Report, string, error) {
	ops, err := e.operators(func(op types.Operator) bool { return op.Controller == policy })
	if err != nil {
		return types.Report{}, "", err
	}
	if len(ops) == 0 {
		op, ok, err := e.registerOperator(policy)
		if err != nil {
			return types.Report{}, "", err
		}
		if !ok {
			return types.Report{}, "the controller controls no operator and none could register", nil
		}
		ops = append(ops, op)
	}
	op, _ := pick(e.r, ops)
	msg, acc, why := e.reportMsg(op)
	if why != "" {
		return types.Report{}, why, nil
	}
	before, err := e.k.NextReportID.Peek(e.ctx)
	if err != nil {
		return types.Report{}, "", err
	}
	ok, err := e.ensure(acc, msg, e.params.ReportDepositAmount)
	if err != nil || !ok {
		return types.Report{}, "the report could not be filed", err
	}
	reports, err := e.reports(types.ReportStatus_REPORT_STATUS_PENDING, func(r types.Report) bool {
		return r.ReportId >= before && r.Reporter == msg.Reporter && r.OperatorAddress == msg.Operator && r.ServiceType == msg.ServiceType
	})
	if err != nil {
		return types.Report{}, "", err
	}
	report, ok := pick(e.r, reports)
	if !ok {
		return types.Report{}, "the filed report is not pending", nil
	}
	return report, "", nil
}

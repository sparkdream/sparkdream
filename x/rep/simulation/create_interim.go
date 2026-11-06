package simulation

import (
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	commontypes "sparkdream/x/common/types"
	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

// memberInterimTypes are the interim types a member may open (ADJUDICATION,
// JURY_DUTY and EXPERT_TESTIMONY are system-only).
var memberInterimTypes = []types.InterimType{
	types.InterimType_INTERIM_TYPE_DISPUTE_MEDIATION,
	types.InterimType_INTERIM_TYPE_PROJECT_APPROVAL,
	types.InterimType_INTERIM_TYPE_BUDGET_REVIEW,
	types.InterimType_INTERIM_TYPE_CONTRIBUTION_REVIEW,
	types.InterimType_INTERIM_TYPE_AUDIT,
	types.InterimType_INTERIM_TYPE_MENTORSHIP,
	types.InterimType_INTERIM_TYPE_OTHER,
}

// createInterimCandidates returns MsgCreateInterim candidates, one per live
// active member.
func (e *simEnv) createInterimCandidates() ([]candidate, error) {
	ms, err := e.members(nil)
	if err != nil {
		return nil, err
	}
	out := make([]candidate, 0, len(ms))
	for _, ma := range ms {
		out = append(out, candidate{acc: ma.acc, msg: &types.MsgCreateInterim{
			Creator:     ma.addr(),
			InterimType: memberInterimTypes[e.r.Intn(len(memberInterimTypes))],
			Complexity:  commontypes.Saturate[types.InterimComplexity](e.r.Intn(3)),
			Deadline:    e.ctx.BlockHeight() + 50 + int64(e.r.Intn(400)),
		}, spent: math.ZeroInt()})
	}
	return out, nil
}

// SimulateMsgCreateInterim has an active member open interim work assigned to
// itself.
func SimulateMsgCreateInterim(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	label := &types.MsgCreateInterim{}
	return operation(ak, bk, k, txGen, label, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		cands, err := e.createInterimCandidates()
		if err != nil {
			return noOpErr(label, err)
		}
		return e.firstValid(label, cands, 4, "no active member can open interim work")
	})
}

// openInterim returns a PENDING or IN_PROGRESS non-adjudication interim
// accepted by keep, opening one first when there is none.
func (e *simEnv) openInterim(keep func(types.Interim) bool) ([]types.Interim, error) {
	find := func() ([]types.Interim, error) {
		return e.interims(func(it types.Interim) bool {
			return (it.Status == types.InterimStatus_INTERIM_STATUS_PENDING || it.Status == types.InterimStatus_INTERIM_STATUS_IN_PROGRESS) &&
				it.Type != types.InterimType_INTERIM_TYPE_ADJUDICATION && (keep == nil || keep(it))
		})
	}
	its, err := find()
	if err != nil || len(its) > 0 {
		return its, err
	}
	cands, err := e.createInterimCandidates()
	if err != nil {
		return nil, err
	}
	for n, i := range e.r.Perm(len(cands)) {
		if n >= maxPrereqTries {
			break
		}
		ok, err := e.ensure(cands[i].acc, cands[i].msg, math.ZeroInt())
		if err != nil {
			return nil, err
		}
		if ok {
			return find()
		}
	}
	return nil, nil
}

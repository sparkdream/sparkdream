package simulation

import (
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

// SimulateMsgApproveProjectBudget approves a PROPOSED project. Usually a seat
// of the project council's Operations Committee approves a budget below the
// large-project threshold directly; one call in four routes the approval
// (any budget) through an x/commons proposal of a body whose allowlist
// carries the message. With no PROPOSED project a member proposes one first.
func SimulateMsgApproveProjectBudget(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	label := &types.MsgApproveProjectBudget{}
	return operation(ak, bk, k, txGen, label, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		proposed := func() ([]types.Project, error) {
			return e.projects(func(p types.Project) bool { return p.Status == types.ProjectStatus_PROJECT_STATUS_PROPOSED })
		}
		if e.r.Intn(4) == 0 {
			bodies := e.groupsAllowing(sdk.MsgTypeURL(label))
			if len(bodies) > 0 {
				return e.advance(label, bodies, decoder(func() *types.MsgApproveProjectBudget { return &types.MsgApproveProjectBudget{} }),
					func(policy string, taken []sdk.Msg) (sdk.Msg, string, error) {
						covered := takenIDs(taken, func(m *types.MsgApproveProjectBudget) uint64 { return m.ProjectId })
						ps, err := proposed()
						if err != nil {
							return nil, "", err
						}
						if len(ps) == 0 {
							p, ok, err := e.proposeProject(false)
							if err != nil || !ok {
								return nil, "no PROPOSED project and none could be proposed", err
							}
							ps = []types.Project{p}
						}
						for _, i := range e.r.Perm(len(ps)) {
							if covered[ps[i].Id] {
								continue
							}
							budget := randBetween(e.r, dream(1_000), e.params.LargeProjectBudgetThreshold.MulRaw(2))
							msg := &types.MsgApproveProjectBudget{
								Approver:       policy,
								ProjectId:      ps[i].Id,
								ApprovedBudget: PtrInt(budget),
								ApprovedSpark:  PtrInt(math.ZeroInt()),
							}
							if e.dryRun(msg) == nil {
								return msg, "", nil
							}
						}
						return nil, "no PROPOSED project this body may approve", nil
					})
			}
		}
		ps, err := proposed()
		if err != nil {
			return noOpErr(label, err)
		}
		if len(ps) == 0 {
			p, ok, err := e.proposeProject(false)
			if err != nil {
				return noOpErr(label, err)
			}
			if !ok {
				return noOp(label, "no PROPOSED project and none could be proposed")
			}
			ps = []types.Project{p}
		}
		var cands []candidate
		for _, p := range ps {
			cands = append(cands, e.approveCandidates(p, math.ZeroInt())...)
		}
		return e.firstValid(label, cands, 6, "no live Operations Committee seat for a PROPOSED project's council")
	})
}

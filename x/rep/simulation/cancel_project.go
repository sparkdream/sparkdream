package simulation

import (
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

// SimulateMsgCancelProject cancels a project. The creator withdraws a
// PROPOSED project or an ACTIVE permissionless one whose work is closed; an
// approved budget-backed project is cancelled through an x/commons proposal
// of its council or Operations Committee (one call in three).
func SimulateMsgCancelProject(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	label := &types.MsgCancelProject{}
	return operation(ak, bk, k, txGen, label, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		if e.r.Intn(3) == 0 {
			if bodies := e.groupsAllowing(sdk.MsgTypeURL(label)); len(bodies) > 0 {
				return e.advance(label, bodies, decoder(func() *types.MsgCancelProject { return &types.MsgCancelProject{} }),
					func(policy string, taken []sdk.Msg) (sdk.Msg, string, error) {
						covered := takenIDs(taken, func(m *types.MsgCancelProject) uint64 { return m.ProjectId })
						ps, err := e.activeProjects()
						if err != nil {
							return nil, "", err
						}
						for _, i := range e.r.Perm(len(ps)) {
							if covered[ps[i].Id] || ps[i].Permissionless {
								continue
							}
							msg := &types.MsgCancelProject{Creator: policy, ProjectId: ps[i].Id, Reason: randomReason(e.r)}
							if e.dryRun(msg) == nil {
								return msg, "", nil
							}
						}
						return nil, "no ACTIVE budget-backed project this body may cancel", nil
					})
			}
		}
		ps, err := e.projects(func(p types.Project) bool {
			if _, ok := e.account(p.Creator); !ok {
				return false
			}
			return p.Status == types.ProjectStatus_PROJECT_STATUS_PROPOSED ||
				(p.Status == types.ProjectStatus_PROJECT_STATUS_ACTIVE && p.Permissionless)
		})
		if err != nil {
			return noOpErr(label, err)
		}
		if len(ps) == 0 {
			p, ok, err := e.proposeProject(false)
			if err != nil {
				return noOpErr(label, err)
			}
			if ok {
				ps = []types.Project{p}
			}
		}
		cands := make([]candidate, 0, len(ps))
		for _, p := range ps {
			acc, _ := e.account(p.Creator)
			cands = append(cands, candidate{acc: acc, msg: &types.MsgCancelProject{
				Creator: p.Creator, ProjectId: p.Id, Reason: randomReason(e.r),
			}, spent: math.ZeroInt()})
		}
		return e.firstValid(label, cands, 4, "no project its live creator may cancel")
	})
}

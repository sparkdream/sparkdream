package simulation

import (
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

// SimulateMsgAssignInitiative assigns an OPEN initiative: usually a member
// with the tier's reputation takes it (self-assignment), sometimes the
// project's creator or a Commons Operations Committee seat assigns it to
// such a member. With no OPEN initiative one is created first.
func SimulateMsgAssignInitiative(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	label := &types.MsgAssignInitiative{}
	return operation(ak, bk, k, txGen, label, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		in, ok, err := e.openInitiative(nil)
		if err != nil {
			return noOpErr(label, err)
		}
		if !ok {
			return noOp(label, "no OPEN initiative and none could be created")
		}
		cands, err := e.assignCandidates(in)
		if err != nil {
			return noOpErr(label, err)
		}
		if len(cands) == 0 {
			// Try the other OPEN initiatives: an expert or epic tier may
			// have no qualifying member.
			open, err := e.initiatives(func(x types.Initiative) bool {
				return x.Status == types.InitiativeStatus_INITIATIVE_STATUS_OPEN
			})
			if err != nil {
				return noOpErr(label, err)
			}
			for _, i := range e.r.Perm(len(open)) {
				if cands, err = e.assignCandidates(open[i]); err != nil {
					return noOpErr(label, err)
				}
				if len(cands) > 0 {
					in = open[i]
					break
				}
			}
		}
		if e.r.Intn(3) == 0 {
			// Assigned by someone else: the project creator or an ops seat.
			var assigners []simtypes.Account
			if p, ok := e.project(in.ProjectId); ok {
				if acc, ok := e.account(p.Creator); ok {
					assigners = append(assigners, acc)
				}
			}
			assigners = append(assigners, e.liveSeats(commonsOpsCommittee)...)
			if by, ok := pick(e.r, assigners); ok {
				for i := range cands {
					msg, ok := cands[i].msg.(*types.MsgAssignInitiative)
					if ok && msg.Assignee != by.Address.String() {
						cands[i] = candidate{acc: by, msg: &types.MsgAssignInitiative{
							Creator:      by.Address.String(),
							InitiativeId: in.Id,
							Assignee:     msg.Assignee,
						}, spent: math.ZeroInt()}
					}
				}
			}
		}
		return e.firstValid(label, cands, 6, "no member qualifies for the initiative's tier")
	})
}

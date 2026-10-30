package simulation

import (
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

// SimulateMsgApproveInitiative records an approval of a SUBMITTED or
// IN_REVIEW initiative by one of its stakers or a Commons Operations
// Committee seat (never its assignee or project creator); now and then an
// ops seat disapproves, which closes it. With no submitted initiative the
// work of an assigned one is submitted first, and with no staker on it a
// member stakes first.
func SimulateMsgApproveInitiative(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	label := &types.MsgApproveInitiative{}
	return operation(ak, bk, k, txGen, label, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		in, ok, err := e.submittedInitiative(nil)
		if err != nil {
			return noOpErr(label, err)
		}
		if !ok {
			return noOp(label, "no SUBMITTED initiative and none could be submitted")
		}
		creator := ""
		if p, ok := e.project(in.ProjectId); ok {
			creator = p.Creator
		}
		conflicted := func(addr string) bool {
			return e.k.SameMember(e.ctx, addr, in.Assignee) || (creator != "" && e.k.SameMember(e.ctx, addr, creator))
		}
		stakes, err := e.k.GetInitiativeStakes(e.ctx, in.Id)
		if err != nil {
			return noOpErr(label, err)
		}
		var cands []candidate
		approve := func(acc simtypes.Account, yes bool) {
			if conflicted(acc.Address.String()) {
				return
			}
			cands = append(cands, candidate{acc: acc, msg: &types.MsgApproveInitiative{
				Creator:      acc.Address.String(),
				InitiativeId: in.Id,
				Approved:     yes,
				Comments:     randomReason(e.r),
			}, spent: math.ZeroInt()})
		}
		for _, s := range stakes {
			if acc, ok := e.account(s.Staker); ok {
				approve(acc, true)
			}
		}
		for _, acc := range e.liveSeats(commonsOpsCommittee) {
			approve(acc, e.r.Intn(8) != 0)
		}
		return e.firstValid(label, cands, 6, "no staker or ops seat may approve the initiative")
	})
}

package simulation

import (
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

// SimulateMsgCloseInitiative closes an open-ended initiative, signed by its
// project's creator or a Commons Operations Committee seat.
func SimulateMsgCloseInitiative(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	label := &types.MsgCloseInitiative{}
	return operation(ak, bk, k, txGen, label, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		ins, err := e.initiatives(func(in types.Initiative) bool {
			switch in.Status {
			case types.InitiativeStatus_INITIATIVE_STATUS_OPEN,
				types.InitiativeStatus_INITIATIVE_STATUS_ASSIGNED,
				types.InitiativeStatus_INITIATIVE_STATUS_SUBMITTED,
				types.InitiativeStatus_INITIATIVE_STATUS_IN_REVIEW:
				return true
			}
			return false
		})
		if err != nil {
			return noOpErr(label, err)
		}
		ops := e.liveSeats(commonsOpsCommittee)
		var cands []candidate
		for _, in := range ins {
			signers := append([]simtypes.Account{}, ops...)
			if p, ok := e.project(in.ProjectId); ok {
				if acc, ok := e.account(p.Creator); ok {
					signers = append(signers, acc, acc)
				}
			}
			by, ok := pick(e.r, signers)
			if !ok {
				continue
			}
			cands = append(cands, candidate{acc: by, msg: &types.MsgCloseInitiative{
				Creator: by.Address.String(), InitiativeId: in.Id, Reason: randomReason(e.r),
			}, spent: math.ZeroInt()})
		}
		return e.firstValid(label, cands, 4, "no initiative its project creator or an ops seat can close")
	})
}

package simulation

import (
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

// SimulateMsgUnassignInitiative returns an ASSIGNED initiative to OPEN: the
// assignee steps back, or a Commons Operations Committee seat forces it.
func SimulateMsgUnassignInitiative(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	label := &types.MsgUnassignInitiative{}
	return operation(ak, bk, k, txGen, label, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		ins, err := e.initiatives(func(in types.Initiative) bool {
			return in.Status == types.InitiativeStatus_INITIATIVE_STATUS_ASSIGNED
		})
		if err != nil {
			return noOpErr(label, err)
		}
		if len(ins) == 0 {
			in, _, ok, err := e.assignedInitiative(nil)
			if err != nil {
				return noOpErr(label, err)
			}
			if ok {
				ins = []types.Initiative{in}
			}
		}
		ops := e.liveSeats(commonsOpsCommittee)
		var cands []candidate
		for _, in := range ins {
			signers := append([]simtypes.Account{}, ops...)
			if acc, ok := e.account(in.Assignee); ok {
				signers = append(signers, acc, acc) // the assignee most often
			}
			by, ok := pick(e.r, signers)
			if !ok {
				continue
			}
			cands = append(cands, candidate{acc: by, msg: &types.MsgUnassignInitiative{
				Creator: by.Address.String(), InitiativeId: in.Id, Reason: randomReason(e.r),
			}, spent: math.ZeroInt()})
		}
		return e.firstValid(label, cands, 4, "no ASSIGNED initiative its assignee or an ops seat can release")
	})
}

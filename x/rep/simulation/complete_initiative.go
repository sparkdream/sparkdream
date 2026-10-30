package simulation

import (
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

// SimulateMsgCompleteInitiative completes an IN_REVIEW initiative whose
// challenge period has ended and whose conviction meets the completion
// requirements, signed by its assignee or an Operations Committee seat.
func SimulateMsgCompleteInitiative(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	label := &types.MsgCompleteInitiative{}
	return operation(ak, bk, k, txGen, label, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		height := e.ctx.BlockHeight()
		ins, err := e.initiatives(func(in types.Initiative) bool {
			return in.Status == types.InitiativeStatus_INITIATIVE_STATUS_IN_REVIEW &&
				in.ChallengePeriodEnd != 0 && height >= in.ChallengePeriodEnd
		})
		if err != nil {
			return noOpErr(label, err)
		}
		ops := append(e.liveSeats(commonsOpsCommittee), e.liveSeats(technicalOpsCommittee)...)
		var cands []candidate
		for _, in := range ins {
			signers := append([]simtypes.Account{}, ops...)
			if acc, ok := e.account(in.Assignee); ok {
				signers = append(signers, acc, acc)
			}
			by, ok := pick(e.r, signers)
			if !ok {
				continue
			}
			cands = append(cands, candidate{acc: by, msg: &types.MsgCompleteInitiative{
				Creator: by.Address.String(), InitiativeId: in.Id, CompletionNotes: "simulation completion",
			}, spent: math.ZeroInt()})
		}
		return e.firstValid(label, cands, 4, "no IN_REVIEW initiative past its challenge period")
	})
}

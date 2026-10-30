package simulation

import (
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

// SimulateMsgAbandonInterim has an assignee give up an open (non
// adjudication) interim.
func SimulateMsgAbandonInterim(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	label := &types.MsgAbandonInterim{}
	return operation(ak, bk, k, txGen, label, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		its, err := e.interims(func(it types.Interim) bool {
			return (it.Status == types.InterimStatus_INTERIM_STATUS_PENDING || it.Status == types.InterimStatus_INTERIM_STATUS_IN_PROGRESS) &&
				it.Type != types.InterimType_INTERIM_TYPE_ADJUDICATION
		})
		if err != nil {
			return noOpErr(label, err)
		}
		cands := e.interimAssigneeCandidates(its, func(acc simtypes.Account, it types.Interim) *candidate {
			return &candidate{acc: acc, msg: &types.MsgAbandonInterim{
				Creator: acc.Address.String(), InterimId: it.Id, Reason: randomReason(e.r),
			}, spent: math.ZeroInt()}
		})
		return e.firstValid(label, cands, 4, "no open interim with a live assignee")
	})
}

package simulation

import (
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

// SimulateMsgRemoveRecoveryCommitment has a member schedule the removal of
// one of its recovery commitments.
func SimulateMsgRemoveRecoveryCommitment(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	label := &types.MsgRemoveRecoveryCommitment{}
	return operation(ak, bk, k, txGen, label, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		var cands []candidate
		err := e.k.RecoveryCommitments.Walk(e.ctx, nil, func(member string, set types.RecoveryCommitmentSet) (bool, error) {
			acc, ok := e.account(member)
			if !ok || len(set.Commitments) == 0 {
				return false, nil
			}
			c := set.Commitments[e.r.Intn(len(set.Commitments))]
			cands = append(cands, candidate{acc: acc, msg: &types.MsgRemoveRecoveryCommitment{
				Member: member, Commitment: c.Commitment,
			}, spent: math.ZeroInt()})
			return false, nil
		})
		if err != nil {
			return noOpErr(label, err)
		}
		return e.firstValid(label, cands, 4, "no live member holds a recovery commitment")
	})
}

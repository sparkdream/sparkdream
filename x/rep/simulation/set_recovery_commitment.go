package simulation

import (
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

// SimulateMsgSetRecoveryCommitment has a member add a recovery commitment
// (or schedule the replacement of one it holds); it activates after
// recovery_change_delay.
func SimulateMsgSetRecoveryCommitment(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	label := &types.MsgSetRecoveryCommitment{}
	return operation(ak, bk, k, txGen, label, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		ms, err := e.members(nil)
		if err != nil {
			return noOpErr(label, err)
		}
		cands := make([]candidate, 0, len(ms))
		for _, ma := range ms {
			preimage := make([]byte, 32)
			e.r.Read(preimage)
			msg := &types.MsgSetRecoveryCommitment{Member: ma.addr(), Commitment: types.RecoveryCommitmentHash(preimage)}
			if set, found := e.k.GetRecoveryCommitments(e.ctx, ma.addr()); found && len(set.Commitments) > 0 && e.r.Intn(2) == 0 {
				msg.Replaces = set.Commitments[e.r.Intn(len(set.Commitments))].Commitment
			}
			cands = append(cands, candidate{acc: ma.acc, msg: msg, spent: math.ZeroInt()})
		}
		return e.firstValid(label, cands, 4, "no member may set a recovery commitment")
	})
}

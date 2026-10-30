package simulation

import (
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

// SimulateMsgSweepTombstone has the canonical address of a tombstone sweep
// the SPARK left on it. The simulated genesis keeps migrations from
// finalizing (migration_helpers.go), so no tombstone exists and this is a
// NoOp unless the genesis carries one.
func SimulateMsgSweepTombstone(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	label := &types.MsgSweepTombstone{}
	return operation(ak, bk, k, txGen, label, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		var cands []candidate
		err := e.k.Tombstones.Walk(e.ctx, nil, func(addr string, t types.Tombstone) (bool, error) {
			if acc, ok := e.account(t.Canonical); ok {
				cands = append(cands, candidate{acc: acc, msg: &types.MsgSweepTombstone{
					Signer: t.Canonical, Tombstone: addr,
				}, spent: math.ZeroInt()})
			}
			return false, nil
		})
		if err != nil {
			return noOpErr(label, err)
		}
		return e.firstValid(label, cands, 4, "no tombstone (no migration finalizes in the simulation)")
	})
}

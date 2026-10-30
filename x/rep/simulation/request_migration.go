package simulation

import (
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

// SimulateMsgRequestMigration has a member ask to move its identity to a
// fresh live sim account (the KEY path; the new address accepts).
func SimulateMsgRequestMigration(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	label := &types.MsgRequestMigration{}
	return operation(ak, bk, k, txGen, label, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		if !e.params.MigrationEnabled || !e.params.KeyPathEnabled {
			return noOp(label, "the key migration path is disabled")
		}
		srcs, err := e.migrationSources()
		if err != nil {
			return noOpErr(label, err)
		}
		var cands []candidate
		for n, i := range e.r.Perm(len(srcs)) {
			if n >= 6 {
				break
			}
			ma := srcs[i]
			to, ok := pick(e.r, e.freshTargets(ma.addr(), types.MigrationPath_MIGRATION_PATH_KEY))
			if !ok {
				continue
			}
			cands = append(cands, candidate{acc: ma.acc, msg: &types.MsgRequestMigration{
				Member: ma.addr(), NewAddress: to.Address.String(),
			}, spent: math.ZeroInt()})
		}
		return e.firstValid(label, cands, 4, "no member with a fresh target account")
	})
}

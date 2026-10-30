package simulation

import (
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

// SimulateMsgCancelMigration has the old or the new address of a pending
// KEY migration call it off.
func SimulateMsgCancelMigration(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	label := &types.MsgCancelMigration{}
	return operation(ak, bk, k, txGen, label, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		ms, err := e.migrations(func(m types.MemberMigration) bool {
			return m.Path == types.MigrationPath_MIGRATION_PATH_KEY && types.IsPendingMigrationStatus(m.Status)
		})
		if err != nil {
			return noOpErr(label, err)
		}
		var cands []candidate
		for _, m := range ms {
			for _, signer := range []string{m.FromAddress, m.ToAddress} {
				if acc, ok := e.account(signer); ok {
					cands = append(cands, candidate{acc: acc, msg: &types.MsgCancelMigration{
						Signer: signer, MigrationId: m.Id,
					}, spent: math.ZeroInt()})
				}
			}
		}
		return e.firstValid(label, cands, 4, "no pending key migration with a live party")
	})
}

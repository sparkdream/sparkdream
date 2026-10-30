package simulation

import (
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

// acceptCandidates returns MsgAcceptMigration candidates for the REQUESTED
// migrations accepted by keep whose new address is a live sim account.
func (e *simEnv) acceptCandidates(keep func(types.MemberMigration) bool) ([]candidate, error) {
	ms, err := e.migrations(func(m types.MemberMigration) bool {
		_, ok := e.account(m.ToAddress)
		return ok && m.Status == types.MigrationStatus_MIGRATION_STATUS_REQUESTED && (keep == nil || keep(m))
	})
	if err != nil {
		return nil, err
	}
	out := make([]candidate, 0, len(ms))
	for _, m := range ms {
		acc, _ := e.account(m.ToAddress)
		out = append(out, candidate{acc: acc, msg: &types.MsgAcceptMigration{
			NewAddress: m.ToAddress, MigrationId: m.Id,
		}, spent: math.ZeroInt()})
	}
	return out, nil
}

// SimulateMsgAcceptMigration has the new address of a REQUESTED migration
// (key, recovery or council path) prove control by accepting it. The
// simulated genesis keeps accepted migrations from finalizing, so the old
// sim account is never tombstoned (migration_helpers.go).
func SimulateMsgAcceptMigration(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	label := &types.MsgAcceptMigration{}
	return operation(ak, bk, k, txGen, label, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		cands, err := e.acceptCandidates(nil)
		if err != nil {
			return noOpErr(label, err)
		}
		return e.firstValid(label, cands, 4, "no REQUESTED migration to a live sim account")
	})
}

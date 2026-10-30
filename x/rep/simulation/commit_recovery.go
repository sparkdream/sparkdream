package simulation

import (
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

// SimulateMsgCommitRecovery seals a recovery: for a member holding an active
// commitment to the simulation's preimage (registered at onboarding), a
// fresh live sim account commits the seal binding the preimage, the member
// and itself as the new address, escrowing the commit deposit. Now and then
// an account commits an unrelated seal instead, which expires unrevealed.
func SimulateMsgCommitRecovery(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	label := &types.MsgCommitRecovery{}
	return operation(ak, bk, k, txGen, label, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		if !e.params.MigrationEnabled {
			return noOp(label, "migration is disabled")
		}
		deposit := e.params.RecoveryCommitDeposit
		var cands []candidate
		if e.r.Intn(4) != 0 {
			srcs, err := e.migrationSources()
			if err != nil {
				return noOpErr(label, err)
			}
			for _, ma := range srcs {
				if !e.recoverable(ma.addr()) {
					continue
				}
				to, ok := pick(e.r, e.freshTargets(ma.addr(), types.MigrationPath_MIGRATION_PATH_RECOVERY))
				if !ok {
					continue
				}
				sealed := types.RecoverySeal(recoveryPreimage(ma.addr()), ma.addr(), to.Address.String())
				if has, err := e.k.RecoveryCommits.Has(e.ctx, sealed); err != nil {
					return noOpErr(label, err)
				} else if has {
					continue
				}
				cands = append(cands, candidate{acc: to, msg: &types.MsgCommitRecovery{
					Submitter: to.Address.String(), Sealed: sealed,
				}, spent: deposit})
			}
		}
		if len(cands) == 0 {
			acc, ok := pick(e.r, e.live(nil))
			if !ok {
				return noOp(label, "no live sim account")
			}
			sealed := make([]byte, types.RecoveryCommitmentSize)
			e.r.Read(sealed)
			cands = append(cands, candidate{acc: acc, msg: &types.MsgCommitRecovery{
				Submitter: acc.Address.String(), Sealed: sealed,
			}, spent: math.MaxInt(deposit, math.ZeroInt())})
		}
		return e.firstValid(label, cands, 4, "no account can pay the recovery deposit")
	})
}

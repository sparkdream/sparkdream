package simulation

import (
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/rep/keeper"
	"sparkdream/x/rep/types"
)

// SimulateMsgRevealRecovery reveals the preimage behind one of the seals
// SimulateMsgCommitRecovery committed, once it is recovery_reveal_min_blocks
// old: the submitter (the new address) reveals for the member, opening a
// RECOVERY migration the new address then accepts.
func SimulateMsgRevealRecovery(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	label := &types.MsgRevealRecovery{}
	return operation(ak, bk, k, txGen, label, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		var recoverable []string
		if err := e.k.RecoveryCommitments.Walk(e.ctx, nil, func(member string, _ types.RecoveryCommitmentSet) (bool, error) {
			if e.recoverable(member) {
				recoverable = append(recoverable, member)
			}
			return false, nil
		}); err != nil {
			return noOpErr(label, err)
		}
		height, now := e.ctx.BlockHeight(), e.ctx.BlockTime().Unix()
		var cands []candidate
		err := e.k.RecoveryCommits.Walk(e.ctx, nil, func(sealed []byte, c types.RecoveryCommit) (bool, error) {
			if c.ExpiresAt <= now || height-c.Height < e.params.RecoveryRevealMinBlocks {
				return false, nil
			}
			acc, ok := e.account(c.Submitter)
			if !ok {
				return false, nil
			}
			for _, member := range recoverable {
				if sealFor(sealed, member, c.Submitter) {
					cands = append(cands, candidate{acc: acc, msg: &types.MsgRevealRecovery{
						Revealer: c.Submitter, Member: member, NewAddress: c.Submitter, Preimage: recoveryPreimage(member),
					}, spent: math.ZeroInt()})
					break
				}
			}
			return false, nil
		})
		if err != nil {
			return noOpErr(label, err)
		}
		return e.firstValid(label, cands, 4, "no mature simulation seal to reveal")
	})
}

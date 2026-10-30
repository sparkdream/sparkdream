package simulation

import (
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/reveal/keeper"
	"sparkdream/x/reveal/types"
)

// SimulateMsgPropose has an active member at min_proposer_trust_level with
// no proposal cooldown propose a contribution of one to three tranches,
// locking the bond (bond_rate * total valuation) from its unlocked DREAM.
func SimulateMsgPropose(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	return operation(ak, bk, k, txGen, &types.MsgPropose{}, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		candidates := accountsWhere(e.accs, func(a simtypes.Account) bool {
			if e.tombstoned(a.Address.String()) {
				return false
			}
			_, reason := e.proposeMsg(a)
			return reason == ""
		})
		acc, ok := pick(e.r, candidates)
		if !ok {
			return noOp(&types.MsgPropose{}, "no member can propose (trust level, cooldown or bond)")
		}
		msg, reason := e.proposeMsg(acc)
		if reason != "" {
			return noOp(msg, reason)
		}
		return e.submit(acc, msg)
	})
}

package simulation

import (
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/name/keeper"
	"sparkdream/x/name/types"
)

// SimulateMsgSetPrimary sets a name as its signer's primary: the owner, or
// the accepted resolver target. A member without a name registers one first.
func SimulateMsgSetPrimary(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	label := &types.MsgSetPrimary{}
	return operation(ak, bk, k, txGen, label, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		// The accepted-target path, when a live target exists.
		if e.r.Intn(3) == 0 {
			accepted, err := e.namesWhere(func(rec types.NameRecord) bool {
				_, ok := e.account(rec.Target)
				return rec.TargetAccepted && ok
			})
			if err != nil {
				return noOpErr(label, err)
			}
			if rec, ok := pick(e.r, accepted); ok {
				acc, _ := e.account(rec.Target)
				return e.submit(acc, &types.MsgSetPrimary{Authority: rec.Target, Name: rec.Name}, zero())
			}
		}
		rec, owner, ok, err := e.ownedName(anyone, nil)
		if err != nil {
			return noOpErr(label, err)
		}
		if !ok {
			return noOp(label, "no live sim account owns a name and none can register one")
		}
		return e.submit(owner, &types.MsgSetPrimary{Authority: owner.Address.String(), Name: rec.Name}, zero())
	})
}

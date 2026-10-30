package simulation

import (
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/name/keeper"
	"sparkdream/x/name/types"
)

// SimulateMsgTransferName has a name's owner hand an undisputed name to
// another active member below the name cap. A member without a name
// registers one first.
func SimulateMsgTransferName(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	label := &types.MsgTransferName{}
	return operation(ak, bk, k, txGen, label, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		var disputeErr error
		undisputed := func(rec types.NameRecord) bool {
			d, err := e.activeDispute(rec.Name)
			if err != nil {
				disputeErr = err
			}
			return err == nil && !d
		}
		rec, owner, ok, err := e.ownedName(anyone, undisputed)
		if err == nil {
			err = disputeErr
		}
		if err != nil {
			return noOpErr(label, err)
		}
		if !ok {
			return noOp(label, "no live sim account owns an undisputed name and none can register one")
		}
		var capErr error
		recipients := e.live(func(a simtypes.Account) bool {
			addr := a.Address.String()
			if e.sameMember(addr, owner.Address.String()) || !e.isActiveMember(addr) {
				return false
			}
			below, err := e.belowCap(a.Address)
			if err != nil {
				capErr = err
			}
			return below
		})
		if capErr != nil {
			return noOpErr(label, capErr)
		}
		to, ok := pick(e.r, recipients)
		if !ok {
			return noOp(label, "no other active member below the name cap")
		}
		msg := &types.MsgTransferName{Authority: owner.Address.String(), Name: rec.Name, NewOwner: to.Address.String()}
		return e.submit(owner, msg, zero())
	})
}

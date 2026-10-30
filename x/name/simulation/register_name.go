package simulation

import (
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/name/keeper"
	"sparkdream/x/name/types"
)

// SimulateMsgRegisterName registers a fresh name for an active x/rep member
// below the name cap that can pay the registration fee (burned). One draw
// in four scavenges a name whose owner has been inactive past
// expiration_duration instead.
func SimulateMsgRegisterName(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	label := &types.MsgRegisterName{}
	return operation(ak, bk, k, txGen, label, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		regs, err := e.registrants(nil)
		if err != nil {
			return noOpErr(label, err)
		}
		acc, ok := pick(e.r, regs)
		if !ok {
			return noOp(label, "no live active member below the name cap can pay the registration fee")
		}
		signer := acc.Address.String()

		name := ""
		if e.r.Intn(4) == 0 {
			expired, err := e.namesWhere(func(rec types.NameRecord) bool {
				owner, err := sdk.AccAddressFromBech32(rec.Owner)
				return err == nil && !e.sameMember(rec.Owner, signer) && e.k.IsOwnerExpired(e.ctx, owner)
			})
			if err != nil {
				return noOpErr(label, err)
			}
			for _, i := range e.r.Perm(len(expired)) {
				disputed, err := e.activeDispute(expired[i].Name)
				if err != nil {
					return noOpErr(label, err)
				}
				if !disputed {
					name = expired[i].Name
					break
				}
			}
		}
		if name == "" {
			fresh, ok, err := e.freshName()
			if err != nil {
				return noOpErr(label, err)
			}
			if !ok {
				return noOp(label, "generated names collided with registered or blocked ones")
			}
			name = fresh
		}
		msg := &types.MsgRegisterName{Authority: signer, Name: name, Data: randomData(e.r)}
		return e.submit(acc, msg, e.registrationFee())
	})
}

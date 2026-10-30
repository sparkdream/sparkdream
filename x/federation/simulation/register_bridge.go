package simulation

import (
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/federation/keeper"
	"sparkdream/x/federation/types"
)

// SimulateMsgRegisterBridge delivers MsgRegisterBridge: an active x/rep
// member binds itself as bridge operator to an ACTIVE external-protocol
// peer. A new x/service operator escrows the service type's min_bond (the
// tx fee is drawn from what is left); an existing ACTIVE one binds with no
// extra stake. A NoOp when no ACTIVE bridged peer exists (only a policy
// address can activate one) or no member can bind.
func SimulateMsgRegisterBridge(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return operation(ak, bk, k, txGen, &types.MsgRegisterBridge{}, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgRegisterBridge{}
		peers, err := e.peersWhere(activeBridged)
		if err != nil {
			return noOpErr(msg, err)
		}
		p, ok := pick(e.r, peers)
		if !ok {
			return noOp(msg, "no ACTIVE external-protocol peer (activation is policy-only)")
		}
		candidates := accountsWhere(e.live(), func(a simtypes.Account) bool {
			addr := a.Address.String()
			_, has := e.binding(addr, p.Id)
			return !has && e.isActiveMember(addr)
		})
		acc, ok := pick(e.r, candidates)
		if !ok {
			return noOp(msg, "no unbound active member for the peer")
		}
		msg, spent, reason := e.registerBridgeMsg(acc, p)
		if reason != "" {
			return noOp(msg, reason)
		}
		return e.submit(acc, msg, spent)
	})
}

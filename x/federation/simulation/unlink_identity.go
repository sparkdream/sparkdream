package simulation

import (
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/federation/keeper"
	"sparkdream/x/federation/types"
)

// SimulateMsgUnlinkIdentity delivers MsgUnlinkIdentity from the local
// address of an identity link (any status), linking one first through
// MsgLinkIdentity when none exists.
func SimulateMsgUnlinkIdentity(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return operation(ak, bk, k, txGen, &types.MsgUnlinkIdentity{}, func(e *simEnv) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgUnlinkIdentity{}
		l, ok, err := e.ensureLink(func(l types.IdentityLink) bool {
			_, ok := e.account(l.LocalAddress)
			return ok
		}, anyone)
		if err != nil {
			return noOpErr(msg, err)
		}
		if !ok {
			return noOp(msg, "no identity link held by a live sim account (links need an ACTIVE bridged peer)")
		}
		acc, _ := e.account(l.LocalAddress)
		msg.Creator, msg.PeerId = l.LocalAddress, l.PeerId
		return e.submit(acc, msg, math.ZeroInt())
	})
}

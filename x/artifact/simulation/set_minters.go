package simulation

import (
	"math/rand"

	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/artifact/keeper"
	"sparkdream/x/artifact/types"
	commontypes "sparkdream/x/common/types"
)

// SimulateMsgSetMinters adds another sim account as a minter of a random open class, or removes one.
func SimulateMsgSetMinters(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgSetMinters{}
		c, owner, ok := randomOpenClass(r, ctx, k, accs)
		if !ok {
			return noop(msg, "no open class owned by a sim account")
		}
		msg = &types.MsgSetMinters{Owner: owner.Address.String(), ClassId: c.Id}
		if len(c.Minters) > 0 && r.Intn(2) == 0 {
			msg.Remove = []string{c.Minters[r.Intn(len(c.Minters))]}
		} else {
			if commontypes.Saturate[uint32](len(c.Minters)) >= k.GetParams(ctx).MaxMintersPerClass {
				return noop(msg, "minter list full")
			}
			to, ok := otherAccount(r, accs, owner.Address)
			if !ok {
				return noop(msg, "no other account")
			}
			msg.Add = []string{to.Address.String()}
		}
		return deliver(r, app, ctx, ak, bk, txGen, owner, msg, math.ZeroInt())
	}
}

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
)

// SimulateMsgFreezeClassMetadata freezes a random unfrozen class's metadata (rarely: it is permanent).
func SimulateMsgFreezeClassMetadata(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgFreezeClassMetadata{}
		if r.Intn(5) != 0 {
			return noop(msg, "skipped to keep classes mutable")
		}
		c, owner, ok := randomClassWhere(r, ctx, k, accs, func(c types.Class) bool { return !c.MetadataFrozen })
		if !ok {
			return noop(msg, "no unfrozen class")
		}
		msg = &types.MsgFreezeClassMetadata{Owner: owner.Address.String(), ClassId: c.Id}
		return deliver(r, app, ctx, ak, bk, txGen, owner, msg, math.ZeroInt())
	}
}

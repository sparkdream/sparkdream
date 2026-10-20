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

// SimulateMsgLockRoyalty locks the royalty split of a random unlocked class.
func SimulateMsgLockRoyalty(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgLockRoyalty{}
		c, owner, ok := randomOpenClass(r, ctx, k, accs)
		if !ok || c.RoyaltyLocked {
			return noop(msg, "no unlocked class owned by a sim account")
		}
		msg = &types.MsgLockRoyalty{Owner: owner.Address.String(), ClassId: c.Id}
		return deliver(r, app, ctx, ak, bk, txGen, owner, msg, math.ZeroInt())
	}
}

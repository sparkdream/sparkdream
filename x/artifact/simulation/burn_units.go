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

// SimulateMsgBurnUnits burns some free units of a holder-burnable edition.
func SimulateMsgBurnUnits(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgBurnUnits{}
		c, b, holder, ok := randomBalance(r, ctx, k, accs, func(c types.Class, b types.Balance) bool {
			return c.Flags.HolderMayBurn() && b.Amount > 0
		})
		if !ok {
			return noop(msg, "no burnable units")
		}
		msg = &types.MsgBurnUnits{Owner: holder.Address.String(), ClassId: c.Id, EditionId: b.EditionId, Amount: uint64(1 + r.Intn(int(b.Amount)))}
		return deliver(r, app, ctx, ak, bk, txGen, holder, msg, math.ZeroInt())
	}
}

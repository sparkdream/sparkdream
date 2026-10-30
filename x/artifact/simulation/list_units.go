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

// SimulateMsgListUnits lists some free units of a visible edition.
func SimulateMsgListUnits(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgListUnits{}
		c, b, holder, ok := randomBalance(r, ctx, k, accs, func(c types.Class, b types.Balance) bool {
			return c.Flags.Transferable && c.Status == types.ContentStatus_CONTENT_STATUS_ACTIVE && b.Amount > 0
		})
		if !ok {
			return noop(msg, "no listable units")
		}
		msg = &types.MsgListUnits{Seller: holder.Address.String(), ClassId: c.Id, EditionId: b.EditionId,
			Quantity: commontypes.Saturate[uint64](1 + r.Intn(commontypes.Saturate[int](b.Amount))), UnitPrice: sdk.NewInt64Coin(sdk.DefaultBondDenom, int64(1+r.Intn(5))*100_000),
			Duration: int64(60 + r.Intn(3600))}
		return deliver(r, app, ctx, ak, bk, txGen, holder, msg, math.ZeroInt())
	}
}

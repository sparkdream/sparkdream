package simulation

import (
	"math/rand"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/artifact/keeper"
	"sparkdream/x/artifact/types"
)

// SimulateMsgForceBuy force-buys a random Harberger token at its current price.
func SimulateMsgForceBuy(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgForceBuy{}
		now := ctx.BlockTime().Unix()
		t, _, ok := randomToken(r, ctx, k, accs, func(c types.Class, t types.Token) bool {
			return c.IsHarberger() && t.Harberger != nil && types.HarbergerForSale(*t.Harberger, now) &&
				c.Status == types.ContentStatus_CONTENT_STATUS_ACTIVE && t.Status == types.ContentStatus_CONTENT_STATUS_ACTIVE
		})
		if !ok {
			return noop(msg, "no harberger token for sale")
		}
		c, err := k.Classes.Get(ctx, t.ClassId)
		if err != nil {
			return noop(msg, "class gone")
		}
		price, _ := types.CurrentHarbergerPrice(c.Harberger, *t.Harberger, now)
		buyer, _ := simtypes.RandomAcc(r, accs)
		if buyer.Address.String() == t.Owner {
			return noop(msg, "picked the holder as buyer")
		}
		msg = &types.MsgForceBuy{Buyer: buyer.Address.String(), ClassId: t.ClassId, TokenId: t.Id,
			MaxPrice: sdk.NewCoin(sdk.DefaultBondDenom, price)}
		return deliver(r, app, ctx, ak, bk, txGen, buyer, msg, price)
	}
}

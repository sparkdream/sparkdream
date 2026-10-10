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

// SimulateMsgBuy buys a random active listing from another account.
func SimulateMsgBuy(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgBuy{}
		if !k.GetParams(ctx).MarketEnabled {
			return noop(msg, "market disabled")
		}
		l, _, ok := randomListing(r, ctx, k, accs)
		if !ok {
			return noop(msg, "no listing")
		}
		c, err := k.Classes.Get(ctx, l.ClassId)
		if err != nil || c.Status != types.ContentStatus_CONTENT_STATUS_ACTIVE {
			return noop(msg, "class not active")
		}
		buyer, _ := simtypes.RandomAcc(r, accs)
		if buyer.Address.String() == l.Seller {
			return noop(msg, "picked the seller")
		}
		msg = &types.MsgBuy{Buyer: buyer.Address.String(), ClassId: l.ClassId, TokenId: l.TokenId, ExpectedPrice: l.Price, ExpectedNonce: l.Nonce}
		return deliver(r, app, ctx, ak, bk, txGen, buyer, msg, l.Price.Amount)
	}
}

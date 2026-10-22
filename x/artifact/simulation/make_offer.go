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

// SimulateMsgMakeOffer escrows an offer on a random sellable token, or a
// collection offer on its class.
func SimulateMsgMakeOffer(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgMakeOffer{}
		p := k.GetParams(ctx)
		if !p.MarketEnabled {
			return noop(msg, "market disabled")
		}
		now := ctx.BlockTime().Unix()
		t, _, ok := randomToken(r, ctx, k, accs, func(c types.Class, t types.Token) bool { return marketableForSim(c, t, now) })
		if !ok {
			return noop(msg, "no token to bid on")
		}
		bidder, _ := simtypes.RandomAcc(r, accs)
		if bidder.Address.String() == t.Owner {
			return noop(msg, "picked the holder as bidder")
		}
		var open uint32
		_ = k.OffersByBidder.Walk(ctx, collectionsStringPrefix(bidder.Address.String()), func(collectionsStringPair) (bool, error) {
			open++
			return false, nil
		})
		if open >= p.MaxOffersPerBidder {
			return noop(msg, "bidder at the open-offer cap")
		}
		price := sdk.NewInt64Coin(sdk.DefaultBondDenom, int64(1+r.Intn(10))*100_000)
		msg = &types.MsgMakeOffer{Bidder: bidder.Address.String(), ClassId: t.ClassId, Price: price, Duration: int64(60 + r.Intn(3600))}
		if r.Intn(3) != 0 {
			hash, err := k.ContentsHash(ctx, t)
			if err != nil {
				return noop(msg, "contents unreadable")
			}
			msg.TokenId, msg.ExpectedContentsHash = t.Id, hash
		}
		return deliver(r, app, ctx, ak, bk, txGen, bidder, msg, price.Amount)
	}
}

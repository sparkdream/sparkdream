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

// SimulateMsgBid bids the minimum on a random open auction.
func SimulateMsgBid(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgBid{}
		if !k.GetParams(ctx).MarketEnabled {
			return noop(msg, "market disabled")
		}
		now := ctx.BlockTime().Unix()
		var a types.Auction
		found := false
		_ = k.Auctions.Walk(ctx, nil, func(_ uint64, au types.Auction) (bool, error) {
			if au.EndsAt > now && r.Intn(2) == 0 {
				a, found = au, true
			}
			return found, nil
		})
		if !found {
			return noop(msg, "no open auction")
		}
		bidder, _ := simtypes.RandomAcc(r, accs)
		if bidder.Address.String() == a.Seller {
			return noop(msg, "picked the seller as bidder")
		}
		c, err := k.Classes.Get(ctx, a.ClassId)
		t, err2 := k.Tokens.Get(ctx, collectionsJoin(a.ClassId, a.TokenId))
		if err != nil || err2 != nil || !marketableForSim(c, t, now) {
			return noop(msg, "auctioned token not sellable")
		}
		hash, err := k.ContentsHash(ctx, t)
		if err != nil {
			return noop(msg, "contents unreadable")
		}
		amount := sdk.NewCoin(sdk.DefaultBondDenom, keeper.MinNextBid(a))
		msg = &types.MsgBid{Bidder: bidder.Address.String(), AuctionId: a.Id, Amount: amount, ExpectedContentsHash: hash}
		return deliver(r, app, ctx, ak, bk, txGen, bidder, msg, amount.Amount)
	}
}

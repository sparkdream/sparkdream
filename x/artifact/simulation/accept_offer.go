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

// SimulateMsgAcceptOffer sells a token to an open offer when its holder is
// a sim account and every precondition holds.
func SimulateMsgAcceptOffer(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgAcceptOffer{}
		if !k.GetParams(ctx).MarketEnabled {
			return noop(msg, "market disabled")
		}
		now := ctx.BlockTime().Unix()
		var o types.Offer
		found := false
		_ = k.Offers.Walk(ctx, nil, func(_ uint64, of types.Offer) (bool, error) {
			if of.ExpiresAt > now && r.Intn(2) == 0 {
				o, found = of, true
			}
			return found, nil
		})
		if !found {
			return noop(msg, "no open offer")
		}
		c, err := k.Classes.Get(ctx, o.ClassId)
		if err != nil {
			return noop(msg, "class gone")
		}
		var t types.Token
		if o.TokenId != 0 {
			if t, err = k.Tokens.Get(ctx, collectionsJoin(o.ClassId, o.TokenId)); err != nil {
				return noop(msg, "token gone")
			}
		} else {
			found = false
			_ = k.Tokens.Walk(ctx, collectionsClassRange(o.ClassId), func(_ keeper.U64Pair, tk types.Token) (bool, error) {
				if tk.Owner != o.Bidder && tk.Descendants == 0 {
					t, found = tk, true
				}
				return found, nil
			})
			if !found {
				return noop(msg, "no token fits the collection offer")
			}
		}
		seller, ok := findAccount(accs, t.Owner)
		if !ok || t.Owner == o.Bidder || !marketableForSim(c, t, now) ||
			(t.Lock != types.TokenLock_TOKEN_LOCK_NONE && t.Lock != types.TokenLock_TOKEN_LOCK_LISTED) {
			return noop(msg, "token not sellable to this offer")
		}
		if o.ExpectedMetadataHash != "" && o.ExpectedMetadataHash != types.MetadataHash(t.Metadata) {
			return noop(msg, "metadata changed since the offer")
		}
		if hash, err := k.ContentsHash(ctx, t); err != nil || hash != o.ExpectedContentsHash {
			return noop(msg, "contents changed since the offer")
		}
		msg = &types.MsgAcceptOffer{Seller: t.Owner, OfferId: o.Id, TokenId: t.Id, ExpectedPrice: o.Price}
		return deliver(r, app, ctx, ak, bk, txGen, seller, msg, math.ZeroInt())
	}
}

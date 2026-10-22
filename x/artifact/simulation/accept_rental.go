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

// SimulateMsgAcceptRental accepts a random open rental offer.
func SimulateMsgAcceptRental(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgAcceptRental{}
		var offer types.RentalOffer
		found := false
		now := ctx.BlockTime().Unix()
		_ = k.RentalOffers.Walk(ctx, nil, func(_ keeper.U64Pair, o types.RentalOffer) (bool, error) {
			if o.ExpiresAt > now && r.Intn(2) == 0 {
				offer, found = o, true
			}
			return found, nil
		})
		if !found {
			return noop(msg, "no open rental offer")
		}
		renter, _ := simtypes.RandomAcc(r, accs)
		if renter.Address.String() == offer.Owner || (offer.User != "" && offer.User != renter.Address.String()) {
			return noop(msg, "offer not open to the picked renter")
		}
		t, err := k.Tokens.Get(ctx, keeper.U64Pair(collectionsJoin(offer.ClassId, offer.TokenId)))
		if err != nil || t.Owner != offer.Owner || t.Lock != types.TokenLock_TOKEN_LOCK_NONE ||
			t.Status != types.ContentStatus_CONTENT_STATUS_ACTIVE || types.IsExpired(t, now) ||
			(t.ExpiresAt > 0 && now+offer.Duration > t.ExpiresAt) {
			return noop(msg, "token moved, locked, hidden or expiring before the rental would end")
		}
		if c, err := k.Classes.Get(ctx, t.ClassId); err != nil || c.Status != types.ContentStatus_CONTENT_STATUS_ACTIVE {
			return noop(msg, "class hidden or gone")
		}
		if offer.Price.Amount.IsPositive() && !k.GetParams(ctx).MarketEnabled {
			return noop(msg, "market disabled")
		}
		hash, _ := k.ContentsHash(ctx, t)
		msg = &types.MsgAcceptRental{Renter: renter.Address.String(), ClassId: offer.ClassId, TokenId: offer.TokenId,
			ExpectedPrice: offer.Price, ExpectedNonce: offer.Nonce, ExpectedContentsHash: hash}
		return deliver(r, app, ctx, ak, bk, txGen, renter, msg, offer.Price.Amount)
	}
}

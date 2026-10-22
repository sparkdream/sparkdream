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

// marketableForSim mirrors the keeper's offer/auction preconditions on a
// class and token so sim ops never send a message the handler rejects.
func marketableForSim(c types.Class, t types.Token, now int64) bool {
	return !c.IsEditions() && c.Flags.Transferable && !c.IsHarberger() &&
		c.Status == types.ContentStatus_CONTENT_STATUS_ACTIVE && t.Status == types.ContentStatus_CONTENT_STATUS_ACTIVE &&
		!types.IsExpired(t, now) && (t.RedeemedAt == 0 || c.Flags.RedeemedTransferable)
}

// SimulateMsgUpdateUnitListing changes the price and quantity of a random
// unit listing within what its seller holds.
func SimulateMsgUpdateUnitListing(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgUpdateUnitListing{}
		if !k.GetParams(ctx).MarketEnabled {
			return noop(msg, "market disabled")
		}
		var l types.UnitListing
		found := false
		_ = k.UnitListings.Walk(ctx, nil, func(_ uint64, ul types.UnitListing) (bool, error) {
			if r.Intn(2) == 0 {
				l, found = ul, true
			}
			return found, nil
		})
		seller, ok := findAccount(accs, l.Seller)
		if !found || !ok {
			return noop(msg, "no unit listing of a sim account")
		}
		c, err := k.Classes.Get(ctx, l.ClassId)
		e, err2 := k.Editions.Get(ctx, collectionsJoin(l.ClassId, l.EditionId))
		b, err3 := k.Balances.Get(ctx, keeper.BalanceKey(collectionsTriple(l.ClassId, l.EditionId, l.Seller)))
		if err != nil || err2 != nil || err3 != nil || c.Status != types.ContentStatus_CONTENT_STATUS_ACTIVE || e.Status != types.ContentStatus_CONTENT_STATUS_ACTIVE {
			return noop(msg, "listing's edition hidden or gone")
		}
		qty := uint64(1 + r.Intn(int(l.Quantity+b.Amount)))
		msg = &types.MsgUpdateUnitListing{Seller: l.Seller, ListingId: l.Id, Quantity: qty,
			UnitPrice: sdk.NewInt64Coin(sdk.DefaultBondDenom, int64(1+r.Intn(5))*100_000), Duration: int64(60 + r.Intn(3600))}
		return deliver(r, app, ctx, ak, bk, txGen, seller, msg, math.ZeroInt())
	}
}

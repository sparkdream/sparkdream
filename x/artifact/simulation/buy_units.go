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

// SimulateMsgBuyUnits buys part of a random active unit listing.
func SimulateMsgBuyUnits(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgBuyUnits{}
		var picked types.UnitListing
		found := false
		now := ctx.BlockTime().Unix()
		_ = k.UnitListings.Walk(ctx, nil, func(_ uint64, l types.UnitListing) (bool, error) {
			if l.ExpiresAt > now && r.Intn(2) == 0 {
				picked, found = l, true
			}
			return found, nil
		})
		if !found {
			return noop(msg, "no active unit listing")
		}
		buyer, _ := simtypes.RandomAcc(r, accs)
		if buyer.Address.String() == picked.Seller {
			return noop(msg, "picked the seller as buyer")
		}
		qty := commontypes.Saturate[uint64](1 + r.Intn(commontypes.Saturate[int](picked.Quantity)))
		msg = &types.MsgBuyUnits{Buyer: buyer.Address.String(), ListingId: picked.Id, Quantity: qty, ExpectedUnitPrice: picked.UnitPrice}
		spend := picked.UnitPrice.Amount.Mul(math.NewIntFromUint64(qty)).Add(k.GetParams(ctx).TokenDeposit)
		return deliver(r, app, ctx, ak, bk, txGen, buyer, msg, spend)
	}
}

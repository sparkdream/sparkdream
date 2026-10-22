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

// SimulateMsgDelistUnits cancels a random unit listing.
func SimulateMsgDelistUnits(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgDelistUnits{}
		var picked types.UnitListing
		var seller simtypes.Account
		found := false
		_ = k.UnitListings.Walk(ctx, nil, func(_ uint64, l types.UnitListing) (bool, error) {
			if acc, ok := findAccount(accs, l.Seller); ok && r.Intn(2) == 0 {
				picked, seller, found = l, acc, true
			}
			return found, nil
		})
		if !found {
			return noop(msg, "no unit listing")
		}
		msg = &types.MsgDelistUnits{Seller: seller.Address.String(), ListingId: picked.Id}
		return deliver(r, app, ctx, ak, bk, txGen, seller, msg, math.ZeroInt())
	}
}

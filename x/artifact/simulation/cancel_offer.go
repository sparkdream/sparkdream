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

// SimulateMsgCancelOffer withdraws a random offer.
func SimulateMsgCancelOffer(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgCancelOffer{}
		var o types.Offer
		found := false
		_ = k.Offers.Walk(ctx, nil, func(_ uint64, of types.Offer) (bool, error) {
			if r.Intn(2) == 0 {
				o, found = of, true
			}
			return found, nil
		})
		bidder, ok := findAccount(accs, o.Bidder)
		if !found || !ok {
			return noop(msg, "no offer of a sim account")
		}
		msg = &types.MsgCancelOffer{Bidder: o.Bidder, OfferId: o.Id}
		return deliver(r, app, ctx, ak, bk, txGen, bidder, msg, math.ZeroInt())
	}
}

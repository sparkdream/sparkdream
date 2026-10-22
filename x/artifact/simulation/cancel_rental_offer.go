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

// SimulateMsgCancelRentalOffer withdraws a random rental offer.
func SimulateMsgCancelRentalOffer(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgCancelRentalOffer{}
		var offer types.RentalOffer
		found := false
		_ = k.RentalOffers.Walk(ctx, nil, func(_ keeper.U64Pair, o types.RentalOffer) (bool, error) {
			if r.Intn(2) == 0 {
				offer, found = o, true
			}
			return found, nil
		})
		owner, ok := findAccount(accs, offer.Owner)
		if !found || !ok {
			return noop(msg, "no rental offer")
		}
		msg = &types.MsgCancelRentalOffer{Owner: offer.Owner, ClassId: offer.ClassId, TokenId: offer.TokenId}
		return deliver(r, app, ctx, ak, bk, txGen, owner, msg, math.ZeroInt())
	}
}

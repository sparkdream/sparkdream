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

// SimulateMsgEndRental hands a random rental back (as its renter).
func SimulateMsgEndRental(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgEndRental{}
		var rental types.Rental
		found := false
		_ = k.Rentals.Walk(ctx, nil, func(_ keeper.U64Pair, rt types.Rental) (bool, error) {
			if r.Intn(2) == 0 {
				rental, found = rt, true
			}
			return found, nil
		})
		user, ok := findAccount(accs, rental.User)
		if !found || !ok {
			return noop(msg, "no rental")
		}
		msg = &types.MsgEndRental{Signer: rental.User, ClassId: rental.ClassId, TokenId: rental.TokenId}
		return deliver(r, app, ctx, ak, bk, txGen, user, msg, math.ZeroInt())
	}
}

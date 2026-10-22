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

// SimulateMsgCancelAuction withdraws a random auction nobody has bid on.
func SimulateMsgCancelAuction(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgCancelAuction{}
		var a types.Auction
		found := false
		_ = k.Auctions.Walk(ctx, nil, func(_ uint64, au types.Auction) (bool, error) {
			if au.HighBidder == "" && r.Intn(2) == 0 {
				a, found = au, true
			}
			return found, nil
		})
		seller, ok := findAccount(accs, a.Seller)
		if !found || !ok {
			return noop(msg, "no bidless auction of a sim account")
		}
		msg = &types.MsgCancelAuction{Seller: a.Seller, AuctionId: a.Id}
		return deliver(r, app, ctx, ak, bk, txGen, seller, msg, math.ZeroInt())
	}
}

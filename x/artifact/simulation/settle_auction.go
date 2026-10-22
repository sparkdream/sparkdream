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

// SimulateMsgSettleAuction settles a random ended auction.
func SimulateMsgSettleAuction(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgSettleAuction{}
		now := ctx.BlockTime().Unix()
		var a types.Auction
		found := false
		_ = k.Auctions.Walk(ctx, nil, func(_ uint64, au types.Auction) (bool, error) {
			if au.EndsAt <= now {
				a, found = au, true
			}
			return found, nil
		})
		if !found {
			return noop(msg, "no ended auction")
		}
		signer, _ := simtypes.RandomAcc(r, accs)
		msg = &types.MsgSettleAuction{Signer: signer.Address.String(), AuctionId: a.Id}
		return deliver(r, app, ctx, ak, bk, txGen, signer, msg, math.ZeroInt())
	}
}

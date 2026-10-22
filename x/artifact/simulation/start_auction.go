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

// SimulateMsgStartAuction auctions a random unlocked, sellable token.
func SimulateMsgStartAuction(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgStartAuction{}
		if !k.GetParams(ctx).MarketEnabled {
			return noop(msg, "market disabled")
		}
		now := ctx.BlockTime().Unix()
		duration := int64(60 + r.Intn(1800))
		t, owner, ok := randomToken(r, ctx, k, accs, func(c types.Class, t types.Token) bool {
			return marketableForSim(c, t, now) && t.Lock == types.TokenLock_TOKEN_LOCK_NONE &&
				(t.ExpiresAt == 0 || now+duration <= t.ExpiresAt)
		})
		if !ok {
			return noop(msg, "no token to auction")
		}
		msg = &types.MsgStartAuction{Seller: t.Owner, ClassId: t.ClassId, TokenId: t.Id,
			ReservePrice:    sdk.NewInt64Coin(sdk.DefaultBondDenom, int64(1+r.Intn(5))*100_000),
			MinIncrementBps: uint32(r.Intn(1000)), Duration: duration}
		return deliver(r, app, ctx, ak, bk, txGen, owner, msg, math.ZeroInt())
	}
}

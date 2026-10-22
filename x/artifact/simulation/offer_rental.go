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

// SimulateMsgOfferRental offers a random unlocked transferable token for rent.
func SimulateMsgOfferRental(ak types.AuthKeeper, bk types.BankKeeper, k keeper.Keeper, txGen client.TxConfig) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgOfferRental{}
		t, owner, ok := randomToken(r, ctx, k, accs, func(c types.Class, t types.Token) bool {
			return c.Flags.Transferable && !c.IsHarberger() && t.Lock == types.TokenLock_TOKEN_LOCK_NONE &&
				c.Status == types.ContentStatus_CONTENT_STATUS_ACTIVE && t.Status == types.ContentStatus_CONTENT_STATUS_ACTIVE
		})
		if !ok {
			return noop(msg, "no rentable token")
		}
		msg = &types.MsgOfferRental{Owner: owner.Address.String(), ClassId: t.ClassId, TokenId: t.Id,
			Price: sdk.NewInt64Coin(sdk.DefaultBondDenom, int64(r.Intn(3))*200_000), Duration: int64(30 + r.Intn(600)),
			OwnerMayEnd: r.Intn(2) == 0, OfferDuration: int64(60 + r.Intn(600))}
		return deliver(r, app, ctx, ak, bk, txGen, owner, msg, math.ZeroInt())
	}
}

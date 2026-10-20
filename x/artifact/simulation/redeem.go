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

// SimulateMsgRedeem redeems a random unredeemed token of a redeemable class
// as the class owner.
func SimulateMsgRedeem(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgRedeem{}
		now := ctx.BlockTime().Unix()
		t, _, ok := randomToken(r, ctx, k, accs, func(c types.Class, t types.Token) bool {
			return c.Flags.Redeemable && t.RedeemedAt == 0 && !types.IsExpired(t, now) &&
				t.Lock != types.TokenLock_TOKEN_LOCK_PENDING_TRANSFER &&
				c.Status == types.ContentStatus_CONTENT_STATUS_ACTIVE && t.Status == types.ContentStatus_CONTENT_STATUS_ACTIVE
		})
		if !ok {
			return noop(msg, "no redeemable token")
		}
		c, _ := k.Classes.Get(ctx, t.ClassId)
		owner, found := findAccount(accs, c.Owner)
		if !found {
			return noop(msg, "class owner is not a sim account")
		}
		msg = &types.MsgRedeem{Signer: owner.Address.String(), ClassId: t.ClassId, TokenIds: []uint64{t.Id}}
		return deliver(r, app, ctx, ak, bk, txGen, owner, msg, math.ZeroInt())
	}
}

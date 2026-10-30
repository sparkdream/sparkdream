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

// SimulateMsgUpdateListing reprices a random listing of a still-listable token.
func SimulateMsgUpdateListing(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgUpdateListing{}
		if !k.GetParams(ctx).MarketEnabled {
			return noop(msg, "market disabled")
		}
		l, seller, ok := randomListing(r, ctx, k, accs)
		if !ok {
			return noop(msg, "no listing")
		}
		c, err := k.Classes.Get(ctx, l.ClassId)
		if err != nil || c.Status != types.ContentStatus_CONTENT_STATUS_ACTIVE {
			return noop(msg, "class not listable")
		}
		t, err := k.Tokens.Get(ctx, collectionsJoin(l.ClassId, l.TokenId))
		if err != nil || t.Status != types.ContentStatus_CONTENT_STATUS_ACTIVE || types.IsExpired(t, ctx.BlockTime().Unix()) ||
			(t.RedeemedAt != 0 && !c.Flags.RedeemedTransferable) || t.Descendants > 0 {
			return noop(msg, "token not listable")
		}
		msg = &types.MsgUpdateListing{Seller: seller.Address.String(), ClassId: l.ClassId, TokenId: l.TokenId,
			Price:    sdk.NewInt64Coin(sdk.DefaultBondDenom, int64(1+r.Intn(20))*1_000_000),
			Duration: int64(600 + r.Intn(86400))}
		return deliver(r, app, ctx, ak, bk, txGen, seller, msg, math.ZeroInt())
	}
}

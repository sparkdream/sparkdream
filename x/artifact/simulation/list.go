package simulation

import (
	"math/rand"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/artifact/keeper"
	"sparkdream/x/artifact/types"
)

// SimulateMsgList lists a random unlocked, visible, transferable token.
func SimulateMsgList(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgList{}
		if !k.GetParams(ctx).MarketEnabled {
			return noop(msg, "market disabled")
		}
		t, owner, ok := randomToken(r, ctx, k, accs, func(c types.Class, t types.Token) bool {
			return c.Flags.Transferable && t.Lock == types.TokenLock_TOKEN_LOCK_NONE &&
				c.Status == types.ContentStatus_CONTENT_STATUS_ACTIVE && t.Status == types.ContentStatus_CONTENT_STATUS_ACTIVE
		})
		if !ok {
			return noop(msg, "no listable token")
		}
		msg = &types.MsgList{
			Seller: owner.Address.String(), ClassId: t.ClassId, TokenId: t.Id,
			Price:    sdk.NewInt64Coin(sdk.DefaultBondDenom, int64(1+r.Intn(20))*1_000_000),
			Duration: int64(600 + r.Intn(86400)),
		}
		return deliver(r, app, ctx, ak, bk, txGen, owner, msg, math.ZeroInt())
	}
}

// SimulateMsgDelist cancels a random listing.
func SimulateMsgDelist(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgDelist{}
		l, seller, ok := randomListing(r, ctx, k, accs)
		if !ok {
			return noop(msg, "no listing")
		}
		msg = &types.MsgDelist{Seller: seller.Address.String(), ClassId: l.ClassId, TokenId: l.TokenId}
		return deliver(r, app, ctx, ak, bk, txGen, seller, msg, math.ZeroInt())
	}
}

func randomListing(r *rand.Rand, ctx sdk.Context, k keeper.Keeper, accs []simtypes.Account) (types.Listing, simtypes.Account, bool) {
	now := ctx.BlockTime().Unix()
	var ls []types.Listing
	_ = k.Listings.Walk(ctx, nil, func(_ collections.Pair[uint64, uint64], l types.Listing) (bool, error) {
		if l.ExpiresAt > now {
			ls = append(ls, l)
		}
		return len(ls) >= 200, nil
	})
	r.Shuffle(len(ls), func(i, j int) { ls[i], ls[j] = ls[j], ls[i] })
	for _, l := range ls {
		if acc, ok := findAccount(accs, l.Seller); ok {
			return l, acc, true
		}
	}
	return types.Listing{}, simtypes.Account{}, false
}

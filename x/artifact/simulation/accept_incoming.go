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

// randomInboxItem picks an unexpired inbox item addressed to a sim account.
func randomInboxItem(r *rand.Rand, ctx sdk.Context, k keeper.Keeper, accs []simtypes.Account) (types.TokenRef, simtypes.Account, bool) {
	now := ctx.BlockTime().Unix()
	type item struct {
		ref types.TokenRef
		to  string
	}
	var items []item
	_ = k.PendingTransfers.Walk(ctx, nil, func(_ collections.Pair[uint64, uint64], pt types.PendingTransfer) (bool, error) {
		if pt.ExpiresAt > now {
			items = append(items, item{types.TokenRef{ClassId: pt.ClassId, TokenId: pt.TokenId}, pt.To})
		}
		return len(items) >= 200, nil
	})
	_ = k.PendingMints.Walk(ctx, nil, func(_ collections.Pair[uint64, uint64], pm types.PendingMint) (bool, error) {
		if pm.ExpiresAt > now {
			items = append(items, item{types.TokenRef{ClassId: pm.ClassId, TokenId: pm.TokenId}, pm.To})
		}
		return len(items) >= 400, nil
	})
	r.Shuffle(len(items), func(i, j int) { items[i], items[j] = items[j], items[i] })
	for _, it := range items {
		if acc, ok := findAccount(accs, it.to); ok {
			return it.ref, acc, true
		}
	}
	return types.TokenRef{}, simtypes.Account{}, false
}

// SimulateMsgAcceptIncoming accepts a random inbox item.
func SimulateMsgAcceptIncoming(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgAcceptIncoming{}
		ref, acc, ok := randomInboxItem(r, ctx, k, accs)
		if !ok {
			return noop(msg, "empty inboxes")
		}
		msg = &types.MsgAcceptIncoming{Recipient: acc.Address.String(), Entries: []types.AcceptEntry{{ClassId: ref.ClassId, TokenId: ref.TokenId}}}
		return deliver(r, app, ctx, ak, bk, txGen, acc, msg, math.ZeroInt())
	}
}

// SimulateMsgRejectIncoming rejects a random inbox item.
func SimulateMsgRejectIncoming(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := &types.MsgRejectIncoming{}
		ref, acc, ok := randomInboxItem(r, ctx, k, accs)
		if !ok {
			return noop(msg, "empty inboxes")
		}
		msg = &types.MsgRejectIncoming{Recipient: acc.Address.String(), Refs: []types.TokenRef{ref}}
		return deliver(r, app, ctx, ak, bk, txGen, acc, msg, math.ZeroInt())
	}
}

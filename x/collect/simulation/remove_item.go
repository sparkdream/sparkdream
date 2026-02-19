package simulation

import (
	"math/rand"

	"cosmossdk.io/collections"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/collect/keeper"
	"sparkdream/x/collect/types"
)

func SimulateMsgRemoveItem(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		simAccount, _ := simtypes.RandomAcc(r, accs)
		msg := &types.MsgRemoveItem{
			Creator: simAccount.Address.String(),
		}

		item, itemID, err := findItemByOwner(r, ctx, k, simAccount.Address.String())
		if err != nil || item == nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "no item found for owner"), nil, nil
		}

		// Remove the item
		if err := k.Item.Remove(ctx, itemID); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "failed to remove item: "+err.Error()), nil, nil
		}

		// Remove indexes
		_ = k.ItemsByCollection.Remove(ctx, collections.Join(item.CollectionId, itemID))
		_ = k.ItemsByOwner.Remove(ctx, collections.Join(simAccount.Address.String(), itemID))

		// Decrement ItemCount on parent collection
		coll, err := k.Collection.Get(ctx, item.CollectionId)
		if err == nil {
			if coll.ItemCount > 0 {
				coll.ItemCount--
			}
			coll.UpdatedAt = ctx.BlockHeight()
			_ = k.Collection.Set(ctx, item.CollectionId, coll)
		}

		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "ok (direct keeper call)"), nil, nil
	}
}

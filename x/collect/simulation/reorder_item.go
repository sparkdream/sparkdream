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

func SimulateMsgReorderItem(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		simAccount, _ := simtypes.RandomAcc(r, accs)
		msg := &types.MsgReorderItem{
			Creator: simAccount.Address.String(),
		}

		// Find a mutable collection owned by simAccount
		_, collID, err := findMutableCollectionByOwner(r, ctx, k, simAccount.Address.String())
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "no mutable collection found"), nil, nil
		}

		// Collect items in this collection
		type itemEntry struct {
			id   uint64
			item types.Item
		}
		var items []itemEntry
		_ = k.ItemsByCollection.Walk(ctx,
			collections.NewPrefixedPairRange[uint64, uint64](collID),
			func(key collections.Pair[uint64, uint64]) (bool, error) {
				itemID := key.K2()
				item, err := k.Item.Get(ctx, itemID)
				if err != nil {
					return false, nil
				}
				items = append(items, itemEntry{itemID, item})
				return false, nil
			},
		)

		if len(items) < 2 {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "need at least 2 items to reorder"), nil, nil
		}

		// Pick two random distinct items and swap their positions
		i := r.Intn(len(items))
		j := r.Intn(len(items) - 1)
		if j >= i {
			j++
		}

		items[i].item.Position, items[j].item.Position = items[j].item.Position, items[i].item.Position

		if err := k.Item.Set(ctx, items[i].id, items[i].item); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "failed to update item: "+err.Error()), nil, nil
		}
		if err := k.Item.Set(ctx, items[j].id, items[j].item); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "failed to update item: "+err.Error()), nil, nil
		}

		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "ok (direct keeper call)"), nil, nil
	}
}

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

func SimulateMsgRemoveItems(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		simAccount, _ := simtypes.RandomAcc(r, accs)
		msg := &types.MsgRemoveItems{
			Creator: simAccount.Address.String(),
		}

		// Find a mutable collection owned by simAccount
		coll, collID, err := findMutableCollectionByOwner(r, ctx, k, simAccount.Address.String())
		if err != nil || coll == nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "no mutable collection found"), nil, nil
		}

		// Walk items in this collection
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

		if len(items) == 0 {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "no items in collection"), nil, nil
		}

		// Remove 1-2 items (up to what's available)
		numToRemove := r.Intn(2) + 1
		if numToRemove > len(items) {
			numToRemove = len(items)
		}

		// Shuffle and pick first numToRemove
		r.Shuffle(len(items), func(i, j int) { items[i], items[j] = items[j], items[i] })

		for i := 0; i < numToRemove; i++ {
			entry := items[i]
			_ = k.Item.Remove(ctx, entry.id)
			_ = k.ItemsByCollection.Remove(ctx, collections.Join(collID, entry.id))
			_ = k.ItemsByOwner.Remove(ctx, collections.Join(entry.item.AddedBy, entry.id))
		}

		// Decrement ItemCount
		if coll.ItemCount >= uint64(numToRemove) {
			coll.ItemCount -= uint64(numToRemove)
		} else {
			coll.ItemCount = 0
		}
		coll.UpdatedAt = ctx.BlockHeight()
		if err := k.Collection.Set(ctx, collID, *coll); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "failed to update collection: "+err.Error()), nil, nil
		}

		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "ok (direct keeper call)"), nil, nil
	}
}

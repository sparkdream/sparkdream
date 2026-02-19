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

func SimulateMsgDeleteCollection(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		simAccount, _ := simtypes.RandomAcc(r, accs)
		msg := &types.MsgDeleteCollection{
			Creator: simAccount.Address.String(),
		}

		coll, collID, err := findCollectionByOwner(r, ctx, k, simAccount.Address.String())
		if err != nil || coll == nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "no collection found for owner"), nil, nil
		}

		// Remove all items in this collection
		var itemIDs []uint64
		_ = k.ItemsByCollection.Walk(ctx,
			collections.NewPrefixedPairRange[uint64, uint64](collID),
			func(key collections.Pair[uint64, uint64]) (bool, error) {
				itemIDs = append(itemIDs, key.K2())
				return false, nil
			},
		)
		for _, itemID := range itemIDs {
			item, err := k.Item.Get(ctx, itemID)
			if err != nil {
				continue
			}
			_ = k.Item.Remove(ctx, itemID)
			_ = k.ItemsByCollection.Remove(ctx, collections.Join(collID, itemID))
			_ = k.ItemsByOwner.Remove(ctx, collections.Join(item.AddedBy, itemID))
		}

		// Remove all collaborators for this collection
		var collabKeys []string
		_ = k.Collaborator.Walk(ctx, nil, func(key string, collab types.Collaborator) (bool, error) {
			if collab.CollectionId == collID {
				collabKeys = append(collabKeys, key)
			}
			return false, nil
		})
		for _, key := range collabKeys {
			collab, err := k.Collaborator.Get(ctx, key)
			if err != nil {
				continue
			}
			_ = k.Collaborator.Remove(ctx, key)
			_ = k.CollaboratorReverse.Remove(ctx, collections.Join(collab.Address, collab.CollectionId))
		}

		// Remove indexes
		_ = k.CollectionsByOwner.Remove(ctx, collections.Join(coll.Owner, collID))
		_ = k.CollectionsByStatus.Remove(ctx, collections.Join(int32(coll.Status), collID))
		if coll.ExpiresAt > 0 {
			_ = k.CollectionsByExpiry.Remove(ctx, collections.Join(coll.ExpiresAt, collID))
		}

		// Remove the collection itself
		if err := k.Collection.Remove(ctx, collID); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "failed to remove collection: "+err.Error()), nil, nil
		}

		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "ok (direct keeper call)"), nil, nil
	}
}

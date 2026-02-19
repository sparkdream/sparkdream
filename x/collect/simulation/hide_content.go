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

func SimulateMsgHideContent(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		simAccount, _ := simtypes.RandomAcc(r, accs)
		msg := &types.MsgHideContent{
			Creator: simAccount.Address.String(),
		}

		// Pick a different account as the collection owner
		ownerAccount, ok := pickDifferentAccount(r, accs, simAccount.Address.String())
		if !ok {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "not enough accounts"), nil, nil
		}

		collID, err := getOrCreateCollection(r, ctx, k, ownerAccount.Address.String())
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "failed to create collection: "+err.Error()), nil, nil
		}

		// Create flag for the collection
		_, err = getOrCreateFlag(r, ctx, k, types.FlagTargetType_FLAG_TARGET_TYPE_COLLECTION, collID, simAccount.Address.String())
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "failed to create flag: "+err.Error()), nil, nil
		}

		// Create hide record
		_, err = getOrCreateHideRecord(r, ctx, k, types.FlagTargetType_FLAG_TARGET_TYPE_COLLECTION, collID, simAccount.Address.String())
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "failed to create hide record: "+err.Error()), nil, nil
		}

		// Update collection status to HIDDEN
		coll, err := k.Collection.Get(ctx, collID)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "failed to get collection: "+err.Error()), nil, nil
		}

		oldStatus := coll.Status
		coll.Status = types.CollectionStatus_COLLECTION_STATUS_HIDDEN

		// Remove old status index, add new
		k.CollectionsByStatus.Remove(ctx, collections.Join(int32(oldStatus), collID)) //nolint:errcheck
		if err := k.CollectionsByStatus.Set(ctx, collections.Join(int32(coll.Status), collID)); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "failed to update status index: "+err.Error()), nil, nil
		}

		if err := k.Collection.Set(ctx, collID, coll); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "failed to update collection: "+err.Error()), nil, nil
		}

		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "ok (direct keeper call)"), nil, nil
	}
}

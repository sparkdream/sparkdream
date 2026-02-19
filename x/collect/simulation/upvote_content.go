package simulation

import (
	"math/rand"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/collect/keeper"
	"sparkdream/x/collect/types"
)

func SimulateMsgUpvoteContent(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		simAccount, _ := simtypes.RandomAcc(r, accs)
		msg := &types.MsgUpvoteContent{
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

		// Get the collection to update upvote count
		coll, err := k.Collection.Get(ctx, collID)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "failed to get collection: "+err.Error()), nil, nil
		}

		// Set dedup key (value 1 = upvote)
		dedupKey := keeper.ReactionDedupCompositeKey(simAccount.Address.String(), types.FlagTargetType_FLAG_TARGET_TYPE_COLLECTION, collID)
		if err := k.ReactionDedup.Set(ctx, dedupKey, 1); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "failed to set dedup key: "+err.Error()), nil, nil
		}

		// Increment upvote count
		coll.UpvoteCount++
		if err := k.Collection.Set(ctx, collID, coll); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "failed to update collection: "+err.Error()), nil, nil
		}

		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "ok (direct keeper call)"), nil, nil
	}
}

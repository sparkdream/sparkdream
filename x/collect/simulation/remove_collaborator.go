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

func SimulateMsgRemoveCollaborator(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		simAccount, _ := simtypes.RandomAcc(r, accs)
		msg := &types.MsgRemoveCollaborator{
			Creator: simAccount.Address.String(),
		}

		collab, collabKey, err := findAnyCollaborator(r, ctx, k)
		if err != nil || collab == nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "no collaborator found"), nil, nil
		}

		// Verify the collection owner is a sim account
		coll, err := k.Collection.Get(ctx, collab.CollectionId)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "collection not found"), nil, nil
		}
		_, found := getAccountForAddress(coll.Owner, accs)
		if !found {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "collection owner not a sim account"), nil, nil
		}

		// Remove the collaborator
		if err := k.Collaborator.Remove(ctx, collabKey); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "failed to remove collaborator: "+err.Error()), nil, nil
		}
		_ = k.CollaboratorReverse.Remove(ctx, collections.Join(collab.Address, collab.CollectionId))

		// Decrement CollaboratorCount on the collection
		if coll.CollaboratorCount > 0 {
			coll.CollaboratorCount--
		}
		coll.UpdatedAt = ctx.BlockHeight()
		_ = k.Collection.Set(ctx, collab.CollectionId, coll)

		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "ok (direct keeper call)"), nil, nil
	}
}

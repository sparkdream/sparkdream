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

func SimulateMsgRateCollection(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		simAccount, _ := simtypes.RandomAcc(r, accs)
		msg := &types.MsgRateCollection{
			Creator: simAccount.Address.String(),
		}

		// Register curator
		if err := getOrCreateCurator(r, ctx, k, simAccount.Address.String()); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "failed to register curator: "+err.Error()), nil, nil
		}

		// Pick a different account as collection owner (curator should not own the collection)
		ownerAccount, ok := pickDifferentAccount(r, accs, simAccount.Address.String())
		if !ok {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "not enough accounts"), nil, nil
		}

		// Create or find a collection owned by a different account
		collID, err := getOrCreateCollection(r, ctx, k, ownerAccount.Address.String())
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "failed to create collection: "+err.Error()), nil, nil
		}

		// Create or find a curation review
		_, err = getOrCreateCurationReview(r, ctx, k, simAccount.Address.String(), collID)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "failed to create curation review: "+err.Error()), nil, nil
		}

		// Increment curator's TotalReviews
		curator, err := k.Curator.Get(ctx, simAccount.Address.String())
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "failed to get curator: "+err.Error()), nil, nil
		}
		curator.TotalReviews++
		if err := k.Curator.Set(ctx, simAccount.Address.String(), curator); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "failed to update curator: "+err.Error()), nil, nil
		}

		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "ok (direct keeper call)"), nil, nil
	}
}

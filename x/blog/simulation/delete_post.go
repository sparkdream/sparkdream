package simulation

import (
	"math/rand"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	"github.com/cosmos/cosmos-sdk/x/simulation"

	"sparkdream/x/blog/keeper"
	"sparkdream/x/blog/types"
)

func SimulateMsgDeletePost(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		// 1. Get the count of posts
		count := k.GetPostCount(ctx)
		if count == 0 {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgDeletePost{}), "no posts to delete"), nil, nil
		}

		// 2. Pick a random post ID
		// We try a few times to find a valid post ID, as IDs might not be contiguous if deletes happened.
		var post types.Post
		var found bool
		for i := 0; i < 100; i++ {
			postID := r.Uint64() % count
			post, found = k.GetPost(ctx, postID)
			if found {
				break
			}
		}

		if !found {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgDeletePost{}), "post not found"), nil, nil
		}

		// 3. Find the simulation account that owns this post
		// We need the private key to sign the deletion transaction, so we must find the owner
		// in the list of 'accs' provided by the simulation framework.
		creatorAddr, err := sdk.AccAddressFromBech32(post.Creator)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgDeletePost{}), "invalid creator address"), nil, err
		}

		simAccount, found := simtypes.FindAccount(accs, creatorAddr)
		if !found {
			// The creator might be a module account or a genesis account not part of the sim list
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgDeletePost{}), "creator not found in simulation accounts"), nil, nil
		}

		// 4. Create the message
		msg := &types.MsgDeletePost{
			Creator: simAccount.Address.String(),
			Id:      post.Id,
		}

		// 5. Construct the OperationInput struct
		opMsg := simulation.OperationInput{
			R:               r,
			App:             app,
			TxGen:           txGen,
			Cdc:             nil,
			Msg:             msg,
			CoinsSpentInMsg: nil,
			Context:         ctx,
			SimAccount:      simAccount,
			AccountKeeper:   ak,
			Bankkeeper:      bk,
			ModuleName:      types.ModuleName,
		}

		// 6. Execute
		return simulation.GenAndDeliverTxWithRandFees(opMsg)
	}
}

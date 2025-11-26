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

func SimulateMsgUpdatePost(
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
			// No posts to update
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgUpdatePost{}), "no posts to update"), nil, nil
		}

		// 2. Pick a random post ID
		var post types.Post
		var found bool
		// Try to find a valid post. If posts were deleted, IDs might be sparse.
		for i := 0; i < 100; i++ {
			postID := r.Uint64() % count
			post, found = k.GetPost(ctx, postID)
			if found {
				break
			}
		}

		if !found {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgUpdatePost{}), "post not found"), nil, nil
		}

		// 3. Find the simulation account that owns this post
		creatorAddr, err := sdk.AccAddressFromBech32(post.Creator)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgUpdatePost{}), "invalid creator address"), nil, err
		}

		simAccount, found := simtypes.FindAccount(accs, creatorAddr)
		if !found {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgUpdatePost{}), "creator not found in simulation accounts"), nil, nil
		}

		// 4. Create the Update message with new random content
		msg := &types.MsgUpdatePost{
			Creator: simAccount.Address.String(),
			Id:      post.Id,
			Title:   simtypes.RandStringOfLength(r, 25),  // New title
			Body:    simtypes.RandStringOfLength(r, 250), // New body
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

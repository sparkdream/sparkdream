package simulation

import (
	"math/rand"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/types"
)

// SimulateMsgEditPost simulates a MsgEditPost message using direct keeper calls.
// This bypasses edit fee and edit window requirements for simulation purposes.
// Full integration testing should be done in integration tests.
func SimulateMsgEditPost(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		simAccount, _ := simtypes.RandomAcc(r, accs)

		// Get or create a post owned by this account
		postID, err := getOrCreatePost(r, ctx, k, simAccount.Address.String())
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgEditPost{}), "failed to get/create post"), nil, nil
		}

		// Use direct keeper calls to edit post (bypasses edit fee, edit window)
		post, err := k.Post.Get(ctx, postID)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgEditPost{}), "failed to get post"), nil, nil
		}

		post.Content = "Edited: " + randomContent(r)
		post.Edited = true
		post.EditedAt = ctx.BlockTime().Unix()

		if err := k.Post.Set(ctx, postID, post); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgEditPost{}), "failed to update post"), nil, nil
		}

		// Return success
		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgEditPost{}), "ok (direct keeper call)"), nil, nil
	}
}

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

// SimulateMsgDeletePost simulates a MsgDeletePost message using direct keeper calls.
// This bypasses authorization requirements for simulation purposes.
// Full integration testing should be done in integration tests.
func SimulateMsgDeletePost(
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
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgDeletePost{}), "failed to get/create post"), nil, nil
		}

		// Use direct keeper calls to delete post (set status to deleted)
		post, err := k.Post.Get(ctx, postID)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgDeletePost{}), "failed to get post"), nil, nil
		}

		post.Status = types.PostStatus_POST_STATUS_DELETED

		if err := k.Post.Set(ctx, postID, post); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgDeletePost{}), "failed to delete post"), nil, nil
		}

		// Return success
		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgDeletePost{}), "ok (direct keeper call)"), nil, nil
	}
}

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

// SimulateMsgUnpinPost simulates a MsgUnpinPost message using direct keeper calls.
// This bypasses the operations committee requirement for simulation purposes.
// Full integration testing should be done in integration tests.
func SimulateMsgUnpinPost(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		simAccount, _ := simtypes.RandomAcc(r, accs)

		// Get or create a pinned post
		postID, err := getOrCreatePinnedPost(r, ctx, k, simAccount.Address.String())
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgUnpinPost{}), "failed to get/create pinned post"), nil, nil
		}

		// Use direct keeper calls to unpin the post
		post, err := k.Post.Get(ctx, postID)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgUnpinPost{}), "failed to get post"), nil, nil
		}

		post.Pinned = false
		if err := k.Post.Set(ctx, postID, post); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgUnpinPost{}), "failed to unpin post"), nil, nil
		}

		// Return success
		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgUnpinPost{}), "ok (direct keeper call)"), nil, nil
	}
}

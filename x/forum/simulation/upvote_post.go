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

// SimulateMsgUpvotePost simulates a MsgUpvotePost message using direct keeper calls.
// This bypasses spam tax requirements for simulation purposes.
// Full integration testing should be done in integration tests.
func SimulateMsgUpvotePost(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		simAccount, _ := simtypes.RandomAcc(r, accs)

		// Find or create a post to upvote
		otherAccount, _ := simtypes.RandomAcc(r, accs)
		if otherAccount.Address.String() == simAccount.Address.String() {
			// Try to get a different account
			for i := 0; i < len(accs); i++ {
				if accs[i].Address.String() != simAccount.Address.String() {
					otherAccount = accs[i]
					break
				}
			}
		}

		targetPostID, err := getOrCreatePost(r, ctx, k, otherAccount.Address.String())
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgUpvotePost{}), "failed to create post"), nil, nil
		}

		// Use direct keeper calls to upvote (bypasses spam tax)
		post, err := k.Post.Get(ctx, targetPostID)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgUpvotePost{}), "failed to get post"), nil, nil
		}

		post.UpvoteCount++

		if err := k.Post.Set(ctx, targetPostID, post); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgUpvotePost{}), "failed to upvote post"), nil, nil
		}

		// Return success
		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgUpvotePost{}), "ok (direct keeper call)"), nil, nil
	}
}

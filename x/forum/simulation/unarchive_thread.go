package simulation

import (
	"fmt"
	"math/rand"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/types"
)

// SimulateMsgUnarchiveThread simulates a MsgUnarchiveThread message using direct keeper calls.
// This bypasses fee requirements and cooldown checks for simulation purposes.
// Full integration testing should be done in integration tests.
func SimulateMsgUnarchiveThread(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		simAccount, _ := simtypes.RandomAcc(r, accs)

		// Get or create an archived thread (status-flag based)
		archiveID, err := getOrCreateArchivedThread(r, ctx, k, simAccount.Address.String())
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgUnarchiveThread{}), "failed to get/create archived thread"), nil, nil
		}

		// Load the archived root post
		rootPost, err := k.Post.Get(ctx, archiveID)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgUnarchiveThread{}), "root post not found"), nil, nil
		}

		// Restore root post status
		rootPost.Status = types.PostStatus_POST_STATUS_ACTIVE
		if err := k.Post.Set(ctx, archiveID, rootPost); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgUnarchiveThread{}), "failed to restore root post"), nil, nil
		}

		// Restore all thread posts
		iter, err := k.Post.Iterate(ctx, nil)
		if err == nil {
			defer iter.Close()
			for ; iter.Valid(); iter.Next() {
				post, _ := iter.Value()
				if post.RootId == archiveID && post.PostId != archiveID && post.Status == types.PostStatus_POST_STATUS_ARCHIVED {
					post.Status = types.PostStatus_POST_STATUS_ACTIVE
					k.Post.Set(ctx, post.PostId, post)
				}
			}
		}

		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgUnarchiveThread{}), fmt.Sprintf("ok (direct keeper call, thread %d)", archiveID)), nil, nil
	}
}

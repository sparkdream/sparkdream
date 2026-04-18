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

// SimulateMsgMoveThread simulates a MsgMoveThread message using direct keeper calls.
// This bypasses operations committee and sentinel checks for simulation purposes.
// Full integration testing should be done in integration tests.
func SimulateMsgMoveThread(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		simAccount, _ := simtypes.RandomAcc(r, accs)

		// Get or create a root post to move
		rootID, err := getOrCreateRootPost(r, ctx, k, simAccount.Address.String())
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgMoveThread{}), "failed to get/create root post"), nil, nil
		}

		// Use direct keeper calls to move thread (bypasses operations committee check)
		post, err := k.Post.Get(ctx, rootID)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgMoveThread{}), "post not found"), nil, nil
		}

		newCategoryID := post.CategoryId + 1

		// Create move record
		moveRecord := types.ThreadMoveRecord{
			RootId:             rootID,
			Sentinel:           simAccount.Address.String(),
			OriginalCategoryId: post.CategoryId,
			NewCategoryId:      newCategoryID,
			MovedAt:            ctx.BlockTime().Unix(),
			MoveReason:         "Moving thread for better organization",
		}
		if err := k.ThreadMoveRecord.Set(ctx, rootID, moveRecord); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgMoveThread{}), "failed to create move record"), nil, nil
		}

		// Update the post's category
		post.CategoryId = newCategoryID
		if err := k.Post.Set(ctx, rootID, post); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgMoveThread{}), "failed to move thread"), nil, nil
		}

		// Return success
		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgMoveThread{}), "ok (direct keeper call)"), nil, nil
	}
}

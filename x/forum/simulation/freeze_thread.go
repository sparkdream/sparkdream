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

// SimulateMsgFreezeThread simulates a MsgFreezeThread message using direct keeper calls.
// This bypasses the operations committee requirement for simulation purposes.
// Full integration testing should be done in integration tests.
func SimulateMsgFreezeThread(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		simAccount, _ := simtypes.RandomAcc(r, accs)

		// Find an unlocked root post to freeze
		_, rootID, err := findUnlockedRootPost(r, ctx, k)
		if err != nil {
			// Create one
			rootID, err = getOrCreateRootPost(r, ctx, k, simAccount.Address.String())
			if err != nil {
				return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgFreezeThread{}), "failed to get/create root post"), nil, nil
			}
		}

		// Get the post as value type
		rootPost, err := k.Post.Get(ctx, rootID)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgFreezeThread{}), "failed to get post"), nil, nil
		}

		// Use direct keeper calls to freeze the thread
		// Since there's no FROZEN status, use ARCHIVED to simulate freeze
		rootPost.Status = types.PostStatus_POST_STATUS_ARCHIVED
		rootPost.Locked = true
		rootPost.LockReason = "Thread frozen by operations committee"
		if err := k.Post.Set(ctx, rootID, rootPost); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgFreezeThread{}), "failed to freeze thread"), nil, nil
		}

		// Return success
		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgFreezeThread{}), "ok (direct keeper call)"), nil, nil
	}
}

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

// SimulateMsgLockThread simulates a MsgLockThread message using direct keeper calls.
// This bypasses the operations committee/sentinel requirements for simulation purposes.
// Full integration testing should be done in integration tests.
func SimulateMsgLockThread(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		simAccount, _ := simtypes.RandomAcc(r, accs)

		// Find an unlocked root post to lock
		_, rootID, err := findUnlockedRootPost(r, ctx, k)
		if err != nil {
			// Create one
			rootID, err = getOrCreateRootPost(r, ctx, k, simAccount.Address.String())
			if err != nil {
				return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgLockThread{}), "failed to get/create root post"), nil, nil
			}
		}

		// Get the post as value type
		rootPost, err := k.Post.Get(ctx, rootID)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgLockThread{}), "failed to get post"), nil, nil
		}

		// Use direct keeper calls to lock the thread
		rootPost.Locked = true
		if err := k.Post.Set(ctx, rootID, rootPost); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgLockThread{}), "failed to lock thread"), nil, nil
		}

		// Create lock record
		lockRecord := types.ThreadLockRecord{
			RootId:     rootID,
			Sentinel:   simAccount.Address.String(),
			LockedAt:   ctx.BlockTime().Unix(),
			LockReason: "Simulation test lock",
		}
		if err := k.ThreadLockRecord.Set(ctx, rootID, lockRecord); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgLockThread{}), "failed to create lock record"), nil, nil
		}

		// Return success
		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgLockThread{}), "ok (direct keeper call)"), nil, nil
	}
}

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

// SimulateMsgFollowThread simulates a MsgFollowThread message using direct keeper calls.
// This bypasses any membership requirements for simulation purposes.
// Full integration testing should be done in integration tests.
func SimulateMsgFollowThread(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		simAccount, _ := simtypes.RandomAcc(r, accs)

		// Get or create a root post to follow
		threadID, err := getOrCreateRootPost(r, ctx, k, simAccount.Address.String())
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgFollowThread{}), "failed to get/create root post"), nil, nil
		}

		// Use direct keeper calls to follow thread
		followKey := fmt.Sprintf("%s:%d", simAccount.Address.String(), threadID)

		// Check if already following
		_, err = k.ThreadFollow.Get(ctx, followKey)
		if err == nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgFollowThread{}), "already following this thread"), nil, nil
		}

		// Create follow record
		followRecord := types.ThreadFollow{
			ThreadId: threadID,
			Follower: simAccount.Address.String(),
		}

		if err := k.ThreadFollow.Set(ctx, followKey, followRecord); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgFollowThread{}), "failed to follow thread"), nil, nil
		}

		// Return success
		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgFollowThread{}), "ok (direct keeper call)"), nil, nil
	}
}

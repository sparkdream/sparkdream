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

// SimulateMsgUnfollowThread simulates a MsgUnfollowThread message using direct keeper calls.
// This bypasses any membership requirements for simulation purposes.
// Full integration testing should be done in integration tests.
func SimulateMsgUnfollowThread(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		simAccount, _ := simtypes.RandomAcc(r, accs)

		// Get or create a thread follow for this account
		threadID, err := getOrCreateThreadFollow(r, ctx, k, simAccount.Address.String())
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgUnfollowThread{}), "failed to get/create thread follow"), nil, nil
		}

		// Use direct keeper calls to unfollow thread
		key := fmt.Sprintf("%s:%d", simAccount.Address.String(), threadID)

		// Verify that the account is following
		_, err = k.ThreadFollow.Get(ctx, key)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgUnfollowThread{}), "account not following thread"), nil, nil
		}

		// Remove follow record
		if err := k.ThreadFollow.Remove(ctx, key); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgUnfollowThread{}), "failed to unfollow thread"), nil, nil
		}

		// Return success
		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgUnfollowThread{}), "ok (direct keeper call)"), nil, nil
	}
}

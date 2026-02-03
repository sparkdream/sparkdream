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

// SimulateMsgDismissFlags simulates a MsgDismissFlags message using direct keeper calls.
// This bypasses the authority/sentinel requirement for simulation purposes.
// Full governance integration testing should be done in integration tests.
func SimulateMsgDismissFlags(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		simAccount, _ := simtypes.RandomAcc(r, accs)

		// Find a post with flags or create one
		post, postID, err := findPostWithStatus(r, ctx, k, types.PostStatus_POST_STATUS_ACTIVE)
		if err != nil || post == nil {
			// Create a post if none exists
			postID, err = getOrCreatePost(r, ctx, k, simAccount.Address.String())
			if err != nil {
				return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgDismissFlags{}), "failed to create post"), nil, nil
			}
		}

		// Ensure flag record exists
		_, err = getOrCreatePostFlag(ctx, k, postID)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgDismissFlags{}), "failed to create flag record"), nil, nil
		}

		// Use direct keeper calls to dismiss flags (bypasses authority check)
		// Simply remove the flag record
		if err := k.PostFlag.Remove(ctx, postID); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgDismissFlags{}), "failed to remove flags"), nil, nil
		}

		// Return success
		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgDismissFlags{}), "ok (direct keeper call)"), nil, nil
	}
}

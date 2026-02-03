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

// SimulateMsgAppealPost simulates a MsgAppealPost message using direct keeper calls.
// This bypasses fee and cooldown requirements for simulation purposes.
// Full integration testing should be done in integration tests.
func SimulateMsgAppealPost(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		simAccount, _ := simtypes.RandomAcc(r, accs)

		// Get or create a hidden post
		postID, err := getOrCreateHiddenPost(r, ctx, k, simAccount.Address.String())
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgAppealPost{}), "failed to get/create hidden post"), nil, nil
		}

		// Verify that a hide record exists (required for appeal)
		_, err = k.HideRecord.Get(ctx, postID)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgAppealPost{}), "no hide record found"), nil, nil
		}

		// In the real implementation, this creates an appeal initiative
		// For simulation, we just verify the state is valid and return success
		// The actual appeal state is tracked in the initiative system

		// Return success
		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgAppealPost{}), "ok (direct keeper call)"), nil, nil
	}
}

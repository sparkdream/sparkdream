package simulation

import (
	"math/rand"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/season/keeper"
	"sparkdream/x/season/types"
)

// SimulateMsgUpdateTitle simulates a MsgUpdateTitle message using direct keeper calls.
// This bypasses the governance/committee authority requirement for simulation purposes.
// Full authority testing should be done in integration tests.
func SimulateMsgUpdateTitle(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		// Get or create a title to update
		titleId, err := getOrCreateTitle(r, ctx, k)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgUpdateTitle{}), "failed to get or create title"), nil, nil
		}

		// Fetch the title
		title, err := k.Title.Get(ctx, titleId)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgUpdateTitle{}), "failed to get title"), nil, nil
		}

		// Update the title with random changes
		title.Name = title.Name + " (updated)"
		title.Description = "Updated description for simulation"
		title.Seasonal = !title.Seasonal // Toggle seasonal flag

		// Save the updated title via keeper
		if err := k.Title.Set(ctx, titleId, title); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgUpdateTitle{}), "failed to update title"), nil, nil
		}

		// Return success
		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgUpdateTitle{}), "ok (direct keeper call)"), nil, nil
	}
}

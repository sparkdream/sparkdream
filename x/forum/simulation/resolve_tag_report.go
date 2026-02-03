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

// SimulateMsgResolveTagReport simulates a MsgResolveTagReport message using direct keeper calls.
// This bypasses the authority requirement for simulation purposes.
// Full governance integration testing should be done in integration tests.
func SimulateMsgResolveTagReport(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		simAccount, _ := simtypes.RandomAcc(r, accs)

		// Get or create a tag
		tagName, err := getOrCreateTag(r, ctx, k)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgResolveTagReport{}), "failed to get/create tag"), nil, nil
		}

		// Get or create a tag report for this tag
		err = getOrCreateTagReport(r, ctx, k, tagName, simAccount.Address.String())
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgResolveTagReport{}), "failed to get/create tag report"), nil, nil
		}

		// Use direct keeper calls to resolve the report (bypasses authority check)
		// Simply remove the tag report to mark it as resolved
		// Tag type doesn't have Reserved/Banned fields so we just clean up the report
		if err := k.TagReport.Remove(ctx, tagName); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgResolveTagReport{}), "failed to remove report"), nil, nil
		}

		// Return success
		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgResolveTagReport{}), "ok (direct keeper call)"), nil, nil
	}
}

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

// SimulateMsgDeleteAchievement simulates a MsgDeleteAchievement message using direct keeper calls.
// This bypasses the governance/committee authority requirement for simulation purposes.
// Full authority testing should be done in integration tests.
func SimulateMsgDeleteAchievement(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		// Get or create an achievement to delete
		achievementId, err := getOrCreateAchievement(r, ctx, k)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgDeleteAchievement{}), "failed to get or create achievement"), nil, nil
		}

		// Delete the achievement via keeper
		if err := k.Achievement.Remove(ctx, achievementId); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgDeleteAchievement{}), "failed to delete achievement"), nil, nil
		}

		// Return success
		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgDeleteAchievement{}), "ok (direct keeper call)"), nil, nil
	}
}

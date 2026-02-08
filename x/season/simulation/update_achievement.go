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

// SimulateMsgUpdateAchievement simulates a MsgUpdateAchievement message using direct keeper calls.
// This bypasses the governance/committee authority requirement for simulation purposes.
// Full authority testing should be done in integration tests.
func SimulateMsgUpdateAchievement(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		// Get or create an achievement to update
		achievementId, err := getOrCreateAchievement(r, ctx, k)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgUpdateAchievement{}), "failed to get or create achievement"), nil, nil
		}

		// Fetch the achievement
		achievement, err := k.Achievement.Get(ctx, achievementId)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgUpdateAchievement{}), "failed to get achievement"), nil, nil
		}

		// Update the achievement with random changes
		achievement.Name = achievement.Name + " (updated)"
		achievement.Description = "Updated description for simulation"
		achievement.XpReward = uint64(50 + r.Intn(200))

		// Save the updated achievement via keeper
		if err := k.Achievement.Set(ctx, achievementId, achievement); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgUpdateAchievement{}), "failed to update achievement"), nil, nil
		}

		// Return success
		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgUpdateAchievement{}), "ok (direct keeper call)"), nil, nil
	}
}

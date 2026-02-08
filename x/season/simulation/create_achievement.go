package simulation

import (
	"fmt"
	"math/rand"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/season/keeper"
	"sparkdream/x/season/types"
)

// SimulateMsgCreateAchievement simulates a MsgCreateAchievement message using direct keeper calls.
// This bypasses the governance/committee authority requirement for simulation purposes.
// Full authority testing should be done in integration tests.
func SimulateMsgCreateAchievement(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		// Generate a unique achievement ID
		achievementId := fmt.Sprintf("sim_ach_%d_%d", ctx.BlockHeight(), r.Intn(10000))

		// Check if achievement already exists
		_, err := k.Achievement.Get(ctx, achievementId)
		if err == nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgCreateAchievement{}), "achievement already exists"), nil, nil
		}

		// Create a new achievement using direct keeper call
		achievement := types.Achievement{
			AchievementId:        achievementId,
			Name:                 fmt.Sprintf("Achievement %s", achievementId),
			Description:          "A simulation generated achievement",
			Rarity:               types.Rarity(r.Intn(6) + 1), // COMMON to MYTHIC
			XpReward:             uint64(50 + r.Intn(200)),
			RequirementType:      types.RequirementType(r.Intn(8) + 1),
			RequirementThreshold: uint64(1 + r.Intn(10)),
		}

		// Save the achievement directly via keeper
		if err := k.Achievement.Set(ctx, achievementId, achievement); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgCreateAchievement{}), "failed to create achievement"), nil, nil
		}

		// Return success - using NoOpMsg with "ok" comment to indicate direct keeper call succeeded
		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgCreateAchievement{}), "ok (direct keeper call)"), nil, nil
	}
}

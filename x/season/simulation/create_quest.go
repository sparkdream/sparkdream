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

// SimulateMsgCreateQuest simulates a MsgCreateQuest message using direct keeper calls.
// This bypasses the governance authority requirement for simulation purposes.
// Full authority testing should be done in integration tests.
func SimulateMsgCreateQuest(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		// Generate a unique quest ID
		questId := fmt.Sprintf("sim_quest_%d_%d", ctx.BlockHeight(), r.Intn(10000))

		// Check if quest already exists
		_, err := k.Quest.Get(ctx, questId)
		if err == nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgCreateQuest{}), "quest already exists"), nil, nil
		}

		// Create a new quest using direct keeper call
		quest := types.Quest{
			QuestId:        questId,
			Name:           fmt.Sprintf("Quest %s", questId),
			Description:    "A simulation generated quest",
			XpReward:       uint64(50 + r.Intn(200)),
			Repeatable:     r.Intn(2) == 1,
			CooldownEpochs: uint64(r.Intn(5)),
			Active:         true,
		}

		// Save the quest directly via keeper
		if err := k.Quest.Set(ctx, questId, quest); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgCreateQuest{}), "failed to create quest"), nil, nil
		}

		// Return success - using NoOpMsg with "ok" comment to indicate direct keeper call succeeded
		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgCreateQuest{}), "ok (direct keeper call)"), nil, nil
	}
}

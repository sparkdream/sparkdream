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

// SimulateMsgUpdateQuest simulates a MsgUpdateQuest message using direct keeper calls.
// This bypasses the governance/committee authority requirement for simulation purposes.
// Full authority testing should be done in integration tests.
func SimulateMsgUpdateQuest(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		// Get or create a quest to update
		questId, err := getOrCreateQuest(r, ctx, k)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgUpdateQuest{}), "failed to get or create quest"), nil, nil
		}

		// Fetch the quest
		quest, err := k.Quest.Get(ctx, questId)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgUpdateQuest{}), "failed to get quest"), nil, nil
		}

		// Update the quest with random changes
		quest.Name = quest.Name + " (updated)"
		quest.Description = "Updated description for simulation"
		quest.XpReward = uint64(50 + r.Intn(200))
		quest.Repeatable = r.Intn(2) == 1
		quest.CooldownEpochs = uint64(r.Intn(10))

		// Save the updated quest via keeper
		if err := k.Quest.Set(ctx, questId, quest); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgUpdateQuest{}), "failed to update quest"), nil, nil
		}

		// Return success
		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgUpdateQuest{}), "ok (direct keeper call)"), nil, nil
	}
}

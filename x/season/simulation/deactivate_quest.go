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

// SimulateMsgDeactivateQuest simulates a MsgDeactivateQuest message using direct keeper calls.
// This bypasses the governance authority requirement for simulation purposes.
// Full authority testing should be done in integration tests.
func SimulateMsgDeactivateQuest(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		// Find an active quest to deactivate
		quest, questId, err := findActiveQuest(r, ctx, k)
		if err != nil || quest == nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgDeactivateQuest{}), "no active quest found"), nil, nil
		}

		// Deactivate the quest using direct keeper call
		quest.Active = false

		if err := k.Quest.Set(ctx, questId, *quest); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgDeactivateQuest{}), "failed to deactivate quest"), nil, nil
		}

		// Return success
		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgDeactivateQuest{}), "ok (direct keeper call)"), nil, nil
	}
}

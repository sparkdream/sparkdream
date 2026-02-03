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

// SimulateMsgAppealThreadMove simulates a MsgAppealThreadMove message using direct keeper calls.
// This bypasses fee and cooldown requirements for simulation purposes.
// Full integration testing should be done in integration tests.
func SimulateMsgAppealThreadMove(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		simAccount, _ := simtypes.RandomAcc(r, accs)

		// Get or create a moved thread
		rootID, err := getOrCreateMovedThread(r, ctx, k, simAccount.Address.String())
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgAppealThreadMove{}), "failed to get/create moved thread"), nil, nil
		}

		// Use direct keeper calls to file appeal (bypasses fee and cooldown)
		moveRecord, err := k.ThreadMoveRecord.Get(ctx, rootID)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgAppealThreadMove{}), "no move record to appeal"), nil, nil
		}

		// Set appeal as pending (only AppealPending field exists)
		moveRecord.AppealPending = true

		if err := k.ThreadMoveRecord.Set(ctx, rootID, moveRecord); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgAppealThreadMove{}), "failed to file appeal"), nil, nil
		}

		// Return success
		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgAppealThreadMove{}), "ok (direct keeper call)"), nil, nil
	}
}

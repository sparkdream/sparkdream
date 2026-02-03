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

// SimulateMsgAppealThreadLock simulates a MsgAppealThreadLock message using direct keeper calls.
// This bypasses fee and cooldown requirements for simulation purposes.
// Full integration testing should be done in integration tests.
func SimulateMsgAppealThreadLock(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		simAccount, _ := simtypes.RandomAcc(r, accs)

		// Get or create a locked thread
		rootID, err := getOrCreateLockedThread(r, ctx, k, simAccount.Address.String())
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgAppealThreadLock{}), "failed to get/create locked thread"), nil, nil
		}

		// Use direct keeper calls to file appeal (bypasses fee and cooldown)
		lockRecord, err := k.ThreadLockRecord.Get(ctx, rootID)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgAppealThreadLock{}), "no lock record to appeal"), nil, nil
		}

		// Set appeal as pending (only AppealPending field exists)
		lockRecord.AppealPending = true

		if err := k.ThreadLockRecord.Set(ctx, rootID, lockRecord); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgAppealThreadLock{}), "failed to file appeal"), nil, nil
		}

		// Return success
		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgAppealThreadLock{}), "ok (direct keeper call)"), nil, nil
	}
}

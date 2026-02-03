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

// SimulateMsgDisputePin simulates a MsgDisputePin message using direct keeper calls.
// This bypasses author verification for simulation purposes.
// Full integration testing should be done in integration tests.
func SimulateMsgDisputePin(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		simAccount, _ := simtypes.RandomAcc(r, accs)

		// Get or create a pinned reply with proper metadata
		threadID, replyID, err := getOrCreatePinnedReplyWithMetadata(r, ctx, k, simAccount.Address.String())
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgDisputePin{}), "failed to get/create pinned reply"), nil, nil
		}

		// Use direct keeper calls to dispute pin (bypasses author verification)
		metadata, err := k.ThreadMetadata.Get(ctx, threadID)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgDisputePin{}), "thread metadata not found"), nil, nil
		}

		// Find and update the pinned record (only Disputed field exists)
		found := false
		for _, record := range metadata.PinnedRecords {
			if record.PostId == replyID {
				record.Disputed = true
				found = true
				break
			}
		}

		if !found {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgDisputePin{}), "reply has no pinned record"), nil, nil
		}

		if err := k.ThreadMetadata.Set(ctx, threadID, metadata); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgDisputePin{}), "failed to dispute pin"), nil, nil
		}

		// Return success
		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgDisputePin{}), "ok (direct keeper call)"), nil, nil
	}
}

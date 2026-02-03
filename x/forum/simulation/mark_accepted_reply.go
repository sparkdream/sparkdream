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

// SimulateMsgMarkAcceptedReply simulates a MsgMarkAcceptedReply message using direct keeper calls.
// This bypasses any membership requirements for simulation purposes.
// Full integration testing should be done in integration tests.
func SimulateMsgMarkAcceptedReply(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		simAccount, _ := simtypes.RandomAcc(r, accs)

		// Get or create a root post (thread) owned by this account
		threadID, err := getOrCreateRootPostByAuthor(r, ctx, k, simAccount.Address.String())
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgMarkAcceptedReply{}), "failed to get/create thread"), nil, nil
		}

		// Get or create a reply to mark as accepted
		replyID, err := getOrCreateReply(r, ctx, k, threadID)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgMarkAcceptedReply{}), "failed to get/create reply"), nil, nil
		}

		// Use direct keeper calls to mark the accepted reply (bypasses membership check)
		// Get or create thread metadata
		metadata, err := k.ThreadMetadata.Get(ctx, threadID)
		if err != nil {
			metadata = types.ThreadMetadata{
				ThreadId:       threadID,
				PinnedReplyIds: []uint64{},
				PinnedRecords:  []*types.PinnedReplyRecord{},
			}
		}

		// Check if already has an accepted reply
		if metadata.AcceptedReplyId != 0 {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgMarkAcceptedReply{}), "thread already has accepted reply"), nil, nil
		}

		// Set the accepted reply
		metadata.AcceptedReplyId = replyID

		if err := k.ThreadMetadata.Set(ctx, threadID, metadata); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgMarkAcceptedReply{}), "failed to update metadata"), nil, nil
		}

		// Return success
		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgMarkAcceptedReply{}), "ok (direct keeper call)"), nil, nil
	}
}

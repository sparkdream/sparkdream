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

// SimulateMsgConfirmProposedReply simulates a MsgConfirmProposedReply message using direct keeper calls.
// This simulates confirming a proposed accepted reply by directly updating the thread metadata.
// Full integration testing should be done in integration tests.
func SimulateMsgConfirmProposedReply(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		simAccount, _ := simtypes.RandomAcc(r, accs)

		// Get or create a root post owned by this account
		threadID, err := getOrCreateRootPostByAuthor(r, ctx, k, simAccount.Address.String())
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgConfirmProposedReply{}), "failed to get/create thread"), nil, nil
		}

		// Get or create a reply to use as the accepted reply
		replyID, err := getOrCreateReply(r, ctx, k, threadID)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgConfirmProposedReply{}), "failed to get/create reply"), nil, nil
		}

		// Use direct keeper calls to simulate confirming a proposed reply
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
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgConfirmProposedReply{}), "thread already has accepted reply"), nil, nil
		}

		// Set the accepted reply (simulating confirmation of a proposed reply)
		metadata.AcceptedReplyId = replyID

		if err := k.ThreadMetadata.Set(ctx, threadID, metadata); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgConfirmProposedReply{}), "failed to update metadata"), nil, nil
		}

		// Return success
		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgConfirmProposedReply{}), "ok (direct keeper call)"), nil, nil
	}
}

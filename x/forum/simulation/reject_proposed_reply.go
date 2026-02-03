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

// SimulateMsgRejectProposedReply simulates a MsgRejectProposedReply message using direct keeper calls.
// This simulates rejecting a proposed accepted reply by directly updating the thread metadata.
// Full integration testing should be done in integration tests.
func SimulateMsgRejectProposedReply(
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
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgRejectProposedReply{}), "failed to get/create thread"), nil, nil
		}

		// Get or create a reply (simulates a proposed reply that will be rejected)
		replyID, err := getOrCreateReply(r, ctx, k, threadID)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgRejectProposedReply{}), "failed to get/create reply"), nil, nil
		}

		// Use direct keeper calls to simulate rejecting a proposed reply
		// Get or create thread metadata with a proposed reply
		metadata, err := k.ThreadMetadata.Get(ctx, threadID)
		if err != nil {
			metadata = types.ThreadMetadata{
				ThreadId:        threadID,
				PinnedReplyIds:  []uint64{},
				PinnedRecords:   []*types.PinnedReplyRecord{},
				ProposedReplyId: replyID, // Set a proposed reply to reject
				ProposedAt:      ctx.BlockTime().Unix(),
			}
		} else if metadata.ProposedReplyId == 0 {
			// Set a proposed reply if none exists
			metadata.ProposedReplyId = replyID
			metadata.ProposedAt = ctx.BlockTime().Unix()
		}

		// Reject the proposed reply by clearing it
		metadata.ProposedReplyId = 0
		metadata.ProposedAt = 0

		if err := k.ThreadMetadata.Set(ctx, threadID, metadata); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgRejectProposedReply{}), "failed to update metadata"), nil, nil
		}

		// Return success
		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgRejectProposedReply{}), "ok (direct keeper call)"), nil, nil
	}
}

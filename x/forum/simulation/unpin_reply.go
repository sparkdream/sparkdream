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

// SimulateMsgUnpinReply simulates a MsgUnpinReply message using direct keeper calls.
// This bypasses the operations committee/sentinel requirement for simulation purposes.
// Full integration testing should be done in integration tests.
func SimulateMsgUnpinReply(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		simAccount, _ := simtypes.RandomAcc(r, accs)

		// Get or create a pinned reply
		threadID, replyID, err := getOrCreatePinnedReply(r, ctx, k, simAccount.Address.String())
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgUnpinReply{}), "failed to get/create pinned reply"), nil, nil
		}

		// Use direct keeper calls to unpin the reply (bypasses operations committee check)
		reply, err := k.Post.Get(ctx, replyID)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgUnpinReply{}), "reply not found"), nil, nil
		}

		// Unpin the reply
		reply.Pinned = false
		if err := k.Post.Set(ctx, replyID, reply); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgUnpinReply{}), "failed to update reply"), nil, nil
		}

		// Update thread metadata - remove from pinned records
		metadata, err := k.ThreadMetadata.Get(ctx, threadID)
		if err == nil {
			// Remove from PinnedReplyIds
			newPinnedIds := []uint64{}
			for _, id := range metadata.PinnedReplyIds {
				if id != replyID {
					newPinnedIds = append(newPinnedIds, id)
				}
			}
			metadata.PinnedReplyIds = newPinnedIds

			// Remove from PinnedRecords
			newRecords := []*types.PinnedReplyRecord{}
			for _, record := range metadata.PinnedRecords {
				if record.PostId != replyID {
					newRecords = append(newRecords, record)
				}
			}
			metadata.PinnedRecords = newRecords

			if err := k.ThreadMetadata.Set(ctx, threadID, metadata); err != nil {
				return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgUnpinReply{}), "failed to update metadata"), nil, nil
			}
		}

		// Return success
		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgUnpinReply{}), "ok (direct keeper call)"), nil, nil
	}
}

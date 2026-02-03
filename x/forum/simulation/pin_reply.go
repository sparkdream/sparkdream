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

// SimulateMsgPinReply simulates a MsgPinReply message using direct keeper calls.
// This bypasses the authority/sentinel requirement for simulation purposes.
// Full x/rep integration testing should be done in integration tests.
func SimulateMsgPinReply(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		simAccount, _ := simtypes.RandomAcc(r, accs)

		// Get or create a root post (thread)
		threadID, err := getOrCreateRootPost(r, ctx, k, simAccount.Address.String())
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgPinReply{}), "failed to get/create thread"), nil, nil
		}

		// Get or create a reply to the thread
		replyID, err := getOrCreateReply(r, ctx, k, threadID)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgPinReply{}), "failed to get/create reply"), nil, nil
		}

		// Use direct keeper calls to pin the reply (bypasses authority/sentinel check)
		reply, err := k.Post.Get(ctx, replyID)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgPinReply{}), "failed to get reply"), nil, nil
		}

		// Check if already pinned
		if reply.Pinned {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgPinReply{}), "reply is already pinned"), nil, nil
		}

		// Set reply as pinned
		reply.Pinned = true
		if err := k.Post.Set(ctx, replyID, reply); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgPinReply{}), "failed to update reply"), nil, nil
		}

		// Update thread metadata with pinned record
		metadata, err := k.ThreadMetadata.Get(ctx, threadID)
		if err != nil {
			metadata = types.ThreadMetadata{
				ThreadId:       threadID,
				PinnedReplyIds: []uint64{},
				PinnedRecords:  []*types.PinnedReplyRecord{},
			}
		}

		metadata.PinnedReplyIds = append(metadata.PinnedReplyIds, replyID)
		metadata.PinnedRecords = append(metadata.PinnedRecords, &types.PinnedReplyRecord{
			PostId:        replyID,
			PinnedBy:      simAccount.Address.String(),
			PinnedAt:      ctx.BlockTime().Unix(),
			IsSentinelPin: true,
			Disputed:      false,
		})

		if err := k.ThreadMetadata.Set(ctx, threadID, metadata); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgPinReply{}), "failed to update metadata"), nil, nil
		}

		// Return success
		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgPinReply{}), "ok (direct keeper call)"), nil, nil
	}
}

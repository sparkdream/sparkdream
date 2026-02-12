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

// SimulateMsgHidePost simulates a MsgHidePost message using direct keeper calls.
// This bypasses operations committee/sentinel requirements for simulation purposes.
// Full integration testing should be done in integration tests.
func SimulateMsgHidePost(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		simAccount, _ := simtypes.RandomAcc(r, accs)

		// Find or create a post to hide
		otherAccount, _ := simtypes.RandomAcc(r, accs)
		postID, err := getOrCreatePost(r, ctx, k, otherAccount.Address.String())
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgHidePost{}), "failed to create post"), nil, nil
		}

		// Use direct keeper calls to hide post (bypasses operations committee/sentinel checks)
		post, err := k.Post.Get(ctx, postID)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgHidePost{}), "failed to get post"), nil, nil
		}

		// Check if already hidden
		if post.Status == types.PostStatus_POST_STATUS_HIDDEN {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgHidePost{}), "post already hidden"), nil, nil
		}

		now := ctx.BlockTime().Unix()
		post.Status = types.PostStatus_POST_STATUS_HIDDEN
		post.HiddenBy = simAccount.Address.String()
		post.HiddenAt = now

		if err := k.Post.Set(ctx, postID, post); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgHidePost{}), "failed to hide post"), nil, nil
		}

		// Create hide record
		hideRecord := types.HideRecord{
			PostId:               postID,
			Sentinel:             simAccount.Address.String(),
			HiddenAt:             now,
			SentinelBondSnapshot: "1000",
			ReasonCode:           types.ModerationReason_MODERATION_REASON_SPAM,
			ReasonText:           randomReason(r),
		}
		if err := k.HideRecord.Set(ctx, postID, hideRecord); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgHidePost{}), "failed to create hide record"), nil, nil
		}

		// Return success
		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgHidePost{}), "ok (direct keeper call)"), nil, nil
	}
}

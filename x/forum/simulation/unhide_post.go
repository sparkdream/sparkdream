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

// SimulateMsgUnhidePost reverses a hidden post via direct keeper calls,
// matching the style used by SimulateMsgHidePost and the other reverse-
// action sim ops (UnpinPost, UnlockThread). It skips the sentinel /
// council authorization gate and the parent-category dangling guard that
// the real handler enforces — integration and unit tests cover those
// paths.
//
// To exercise the reversal as faithfully as possible without dragging in
// the full bond / sentinel-activity machinery, the op seeds a post into
// HIDDEN status (using getOrCreatePost + a status flip) and a matching
// HideRecord, then flips it back to ACTIVE and removes the record.
func SimulateMsgUnhidePost(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msgType := sdk.MsgTypeURL(&types.MsgUnhidePost{})

		simAccount, _ := simtypes.RandomAcc(r, accs)

		// Seed a post we can move through HIDDEN -> ACTIVE.
		postID, err := getOrCreatePost(r, ctx, k, simAccount.Address.String())
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "failed to get/create post"), nil, nil
		}

		post, err := k.Post.Get(ctx, postID)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "failed to read post"), nil, nil
		}

		// Force the post into HIDDEN if it isn't already (no-op if it is).
		if post.Status != types.PostStatus_POST_STATUS_HIDDEN {
			post.Status = types.PostStatus_POST_STATUS_HIDDEN
			post.HiddenBy = simAccount.Address.String()
			post.HiddenAt = ctx.BlockTime().Unix()
			if err := k.Post.Set(ctx, postID, post); err != nil {
				return simtypes.NoOpMsg(types.ModuleName, msgType, "failed to seed HIDDEN status"), nil, nil
			}
			// Best-effort matching HideRecord so the reversal can clean it up.
			_ = k.HideRecord.Set(ctx, postID, types.HideRecord{
				PostId:   postID,
				Sentinel: simAccount.Address.String(),
				HiddenAt: post.HiddenAt,
			})
		}

		// Now perform the reversal: flip status, clear hide metadata, drop
		// the HideRecord. Mirrors what MsgUnhidePost does on the happy path
		// minus the auth + bond restoration paths.
		post.Status = types.PostStatus_POST_STATUS_ACTIVE
		post.HiddenBy = ""
		post.HiddenAt = 0
		if err := k.Post.Set(ctx, postID, post); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "failed to flip status"), nil, nil
		}
		_ = k.HideRecord.Remove(ctx, postID)

		return simtypes.NoOpMsg(types.ModuleName, msgType, "ok (direct keeper call)"), nil, nil
	}
}

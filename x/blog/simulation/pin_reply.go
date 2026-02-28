package simulation

import (
	"math/rand"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/blog/keeper"
	"sparkdream/x/blog/types"
)

// SimulateMsgPinReply simulates a MsgPinReply message using direct keeper calls.
func SimulateMsgPinReply(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		simAccount, _ := simtypes.RandomAcc(r, accs)
		msgType := sdk.MsgTypeURL(&types.MsgPinReply{})

		// Find or create a post, then an ephemeral reply on it
		postID, err := getOrCreateAnyActivePost(r, ctx, k, simAccount.Address.String())
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "failed to get-or-create active post"), nil, nil
		}

		replyID, err := getOrCreateEphemeralReply(r, ctx, k, postID, simAccount.Address.String())
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "failed to get-or-create ephemeral reply"), nil, nil
		}

		reply, found := k.GetReply(ctx, replyID)
		if !found {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "reply not found"), nil, nil
		}

		// Remove from expiry index
		if reply.ExpiresAt > 0 {
			k.RemoveFromExpiryIndex(ctx, reply.ExpiresAt, "reply", reply.Id)
		}

		// Pin the reply
		reply.ExpiresAt = 0
		reply.PinnedBy = simAccount.Address.String()
		reply.PinnedAt = ctx.BlockTime().Unix()
		k.SetReply(ctx, reply)

		return simtypes.NoOpMsg(types.ModuleName, msgType, "ok (direct keeper call)"), nil, nil
	}
}

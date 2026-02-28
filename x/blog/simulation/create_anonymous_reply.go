package simulation

import (
	"encoding/hex"
	"math/rand"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	"sparkdream/x/blog/keeper"
	"sparkdream/x/blog/types"
)

// SimulateMsgCreateAnonymousReply simulates a MsgCreateAnonymousReply message using direct keeper calls.
// Anonymous replies require ZK proofs which cannot be generated in simulation, so we use direct state writes.
func SimulateMsgCreateAnonymousReply(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msgType := sdk.MsgTypeURL(&types.MsgCreateAnonymousReply{})

		// Find or create a post to reply to
		otherAccount, _ := simtypes.RandomAcc(r, accs)
		postID, err := getOrCreateAnyActivePost(r, ctx, k, otherAccount.Address.String())
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "failed to get-or-create active post"), nil, nil
		}

		// Create reply with module address as creator
		moduleAddr := authtypes.NewModuleAddress(types.ModuleName).String()
		reply := types.Reply{
			PostId:    postID,
			Creator:   moduleAddr,
			Body:      randomBody(r),
			CreatedAt: ctx.BlockTime().Unix(),
			Status:    types.ReplyStatus_REPLY_STATUS_ACTIVE,
			Depth:     1,
		}
		replyID := k.AppendReply(ctx, reply)

		// Increment post reply count
		post, found := k.GetPost(ctx, postID)
		if found {
			post.ReplyCount++
			k.SetPost(ctx, post)
		}

		// Store anonymous metadata
		fakeNullifier := make([]byte, 32)
		r.Read(fakeNullifier)
		fakeMerkleRoot := make([]byte, 32)
		r.Read(fakeMerkleRoot)

		meta := types.AnonymousPostMetadata{
			ContentId:        replyID,
			Nullifier:        fakeNullifier,
			MerkleRoot:       fakeMerkleRoot,
			ProvenTrustLevel: uint32(r.Intn(3) + 1),
		}
		k.SetAnonymousReplyMeta(ctx, replyID, meta)

		// Record nullifier as used (domain=2 for blog replies)
		nullifierHex := hex.EncodeToString(fakeNullifier)
		entry := types.AnonNullifierEntry{
			UsedAt: ctx.BlockTime().Unix(),
			Domain: 2,
			Scope:  postID,
		}
		k.SetNullifierUsed(ctx, 2, postID, nullifierHex, entry)

		return simtypes.NoOpMsg(types.ModuleName, msgType, "ok (direct keeper call)"), nil, nil
	}
}

package simulation

import (
	"encoding/hex"
	"fmt"
	"math/rand"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/types"
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

		// Find or create a root post to reply to
		otherAccount, _ := simtypes.RandomAcc(r, accs)
		threadID, err := getOrCreateRootPost(r, ctx, k, otherAccount.Address.String())
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "failed to get-or-create root post"), nil, nil
		}

		// Get thread to find category
		thread, err := k.Post.Get(ctx, threadID)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "failed to get thread"), nil, nil
		}

		// Create reply with module address as creator
		moduleAddr := authtypes.NewModuleAddress(types.ModuleName).String()
		postID, err := k.PostSeq.Next(ctx)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "failed to generate post ID"), nil, nil
		}

		reply := types.Post{
			PostId:     postID,
			CategoryId: thread.CategoryId,
			RootId:     threadID,
			ParentId:   threadID,
			Author:     moduleAddr,
			Content:    fmt.Sprintf("Anonymous simulation reply %d", r.Intn(10000)),
			CreatedAt:  ctx.BlockTime().Unix(),
			Status:     types.PostStatus_POST_STATUS_ACTIVE,
			Depth:      1,
		}
		if err := k.Post.Set(ctx, postID, reply); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "failed to store reply"), nil, nil
		}

		// Store anonymous metadata
		fakeNullifier := make([]byte, 32)
		r.Read(fakeNullifier)
		fakeMerkleRoot := make([]byte, 32)
		r.Read(fakeMerkleRoot)

		meta := types.AnonymousPostMetadata{
			ContentId:        postID,
			Nullifier:        fakeNullifier,
			MerkleRoot:       fakeMerkleRoot,
			ProvenTrustLevel: uint32(r.Intn(3) + 1),
		}
		k.SetAnonymousReplyMeta(ctx, postID, meta)

		// Record nullifier as used (domain=4 for forum replies)
		nullifierHex := hex.EncodeToString(fakeNullifier)
		entry := types.AnonNullifierEntry{
			UsedAt: ctx.BlockTime().Unix(),
			Domain: 4,
			Scope:  threadID,
		}
		k.SetNullifierUsed(ctx, 4, threadID, nullifierHex, entry)

		return simtypes.NoOpMsg(types.ModuleName, msgType, "ok (direct keeper call)"), nil, nil
	}
}

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

// SimulateMsgCreateAnonymousPost simulates a MsgCreateAnonymousPost message using direct keeper calls.
// Anonymous posts require ZK proofs which cannot be generated in simulation, so we use direct state writes.
func SimulateMsgCreateAnonymousPost(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msgType := sdk.MsgTypeURL(&types.MsgCreateAnonymousPost{})

		// Create post with module address as creator (mimics real anonymous posting)
		moduleAddr := authtypes.NewModuleAddress(types.ModuleName).String()
		post := types.Post{
			Title:          randomTitle(r),
			Body:           randomBody(r),
			Creator:        moduleAddr,
			RepliesEnabled: true,
			CreatedAt:      ctx.BlockTime().Unix(),
			Status:         types.PostStatus_POST_STATUS_ACTIVE,
		}
		postID := k.AppendPost(ctx, post)

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
		k.SetAnonymousPostMeta(ctx, postID, meta)

		// Record nullifier as used (domain=1 for blog posts)
		nullifierHex := hex.EncodeToString(fakeNullifier)
		entry := types.AnonNullifierEntry{
			UsedAt: ctx.BlockTime().Unix(),
			Domain: 1,
			Scope:  0,
		}
		k.SetNullifierUsed(ctx, 1, 0, nullifierHex, entry)

		return simtypes.NoOpMsg(types.ModuleName, msgType, "ok (direct keeper call)"), nil, nil
	}
}

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

		// Find or create a category
		categoryID, err := getOrCreateCategory(r, ctx, k)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "failed to get-or-create category"), nil, nil
		}

		// Create post with module address as creator (mimics real anonymous posting)
		moduleAddr := authtypes.NewModuleAddress(types.ModuleName).String()
		postID, err := k.PostSeq.Next(ctx)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "failed to generate post ID"), nil, nil
		}

		post := types.Post{
			PostId:     postID,
			CategoryId: categoryID,
			RootId:     postID,
			ParentId:   0,
			Author:     moduleAddr,
			Content:    fmt.Sprintf("Anonymous simulation post %d", r.Intn(10000)),
			CreatedAt:  ctx.BlockTime().Unix(),
			Status:     types.PostStatus_POST_STATUS_ACTIVE,
		}
		if err := k.Post.Set(ctx, postID, post); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "failed to store post"), nil, nil
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
		k.SetAnonymousPostMeta(ctx, postID, meta)

		// Record nullifier as used (domain=3 for forum posts)
		nullifierHex := hex.EncodeToString(fakeNullifier)
		entry := types.AnonNullifierEntry{
			UsedAt: ctx.BlockTime().Unix(),
			Domain: 3,
			Scope:  0,
		}
		k.SetNullifierUsed(ctx, 3, 0, nullifierHex, entry)

		return simtypes.NoOpMsg(types.ModuleName, msgType, "ok (direct keeper call)"), nil, nil
	}
}

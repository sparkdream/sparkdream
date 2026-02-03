package simulation

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"math/rand"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/forum/keeper"
	"sparkdream/x/forum/types"
)

// SimulateMsgUnarchiveThread simulates a MsgUnarchiveThread message using direct keeper calls.
// This bypasses fee requirements and cooldown checks for simulation purposes.
// Full integration testing should be done in integration tests.
func SimulateMsgUnarchiveThread(
	ak types.AuthKeeper,
	bk types.BankKeeper,
	k keeper.Keeper,
	txGen client.TxConfig,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		simAccount, _ := simtypes.RandomAcc(r, accs)

		// Get or create an archived thread with data
		archiveID, err := getOrCreateArchivedThreadWithData(r, ctx, k, simAccount.Address.String())
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgUnarchiveThread{}), "failed to get/create archived thread"), nil, nil
		}

		// Use direct keeper calls to unarchive thread (bypasses fee and cooldown)
		archive, err := k.ArchivedThread.Get(ctx, archiveID)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgUnarchiveThread{}), "archived thread not found"), nil, nil
		}

		// Decompress the data
		var posts []types.Post
		if len(archive.CompressedData) > 0 {
			gzReader, gzErr := gzip.NewReader(bytes.NewReader(archive.CompressedData))
			if gzErr == nil {
				decoder := json.NewDecoder(gzReader)
				decoder.Decode(&posts)
				gzReader.Close()
			}
		}

		// Restore posts (or create a new one if decompression failed)
		if len(posts) == 0 {
			// Create a new root post
			categoryID, _ := getOrCreateCategory(r, ctx, k)
			post := types.Post{
				PostId:     archiveID,
				CategoryId: categoryID,
				RootId:     archiveID,
				ParentId:   0,
				Author:     simAccount.Address.String(),
				Content:    fmt.Sprintf("Unarchived post content %d", r.Intn(10000)),
				CreatedAt:  ctx.BlockTime().Unix(),
				Status:     types.PostStatus_POST_STATUS_ACTIVE,
			}
			posts = append(posts, post)
		}

		// Restore each post with ACTIVE status
		for _, post := range posts {
			post.Status = types.PostStatus_POST_STATUS_ACTIVE
			if err := k.Post.Set(ctx, post.PostId, post); err != nil {
				return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgUnarchiveThread{}), "failed to restore post"), nil, nil
			}
		}

		// Remove the archive record
		if err := k.ArchivedThread.Remove(ctx, archiveID); err != nil {
			return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgUnarchiveThread{}), "failed to remove archive"), nil, nil
		}

		// Return success
		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(&types.MsgUnarchiveThread{}), "ok (direct keeper call)"), nil, nil
	}
}

// getOrCreateArchivedThreadWithData creates an archived thread with properly compressed data
func getOrCreateArchivedThreadWithData(r *rand.Rand, ctx sdk.Context, k keeper.Keeper, author string) (uint64, error) {
	// Try to find existing archived thread with data
	iter, err := k.ArchivedThread.Iterate(ctx, nil)
	if err == nil {
		defer iter.Close()
		for ; iter.Valid(); iter.Next() {
			archive, _ := iter.Value()
			key, _ := iter.Key()
			if len(archive.CompressedData) > 0 {
				return key, nil
			}
		}
	}

	// Create a new archived thread with data
	return createArchivedThreadWithData(r, ctx, k, author)
}

// createArchivedThreadWithData creates an archived thread with properly compressed data
func createArchivedThreadWithData(r *rand.Rand, ctx sdk.Context, k keeper.Keeper, author string) (uint64, error) {
	// Create a root post
	postID, err := k.PostSeq.Next(ctx)
	if err != nil {
		return 0, err
	}

	categoryID, _ := getOrCreateCategory(r, ctx, k)

	post := types.Post{
		PostId:     postID,
		CategoryId: categoryID,
		RootId:     postID,
		ParentId:   0,
		Author:     author,
		Content:    "Archived post content for simulation",
		CreatedAt:  ctx.BlockTime().Unix() - types.DefaultUnarchiveCooldown - 100, // In the past
		Status:     types.PostStatus_POST_STATUS_ARCHIVED,
	}

	// Compress post data
	posts := []types.Post{post}
	jsonData, err := json.Marshal(posts)
	if err != nil {
		return 0, err
	}

	var buf bytes.Buffer
	gzWriter := gzip.NewWriter(&buf)
	if _, err := gzWriter.Write(jsonData); err != nil {
		return 0, err
	}
	gzWriter.Close()

	// Create archived thread record
	archive := types.ArchivedThread{
		RootId:         postID,
		ArchivedAt:     ctx.BlockTime().Unix() - types.DefaultUnarchiveCooldown - 100, // Past cooldown
		PostCount:      1,
		CompressedData: buf.Bytes(),
	}

	if err := k.ArchivedThread.Set(ctx, postID, archive); err != nil {
		return 0, err
	}
	return postID, nil
}

package keeper

import (
	"context"
	"fmt"

	reptypes "sparkdream/x/rep/types"
)

// Compile-time assertion: Keeper satisfies the rep ForumKeeper contract.
var _ reptypes.ForumKeeper = Keeper{}

// PruneTagReferences scans every post and removes the given tag name from
// any post that references it. Invoked by x/rep's ResolveTagReport (action=1).
// Best-effort: iteration or write errors are surfaced but individual post
// update failures do not abort the scan.
func (k Keeper) PruneTagReferences(ctx context.Context, tagName string) error {
	iter, err := k.Post.Iterate(ctx, nil)
	if err != nil {
		return err
	}
	defer iter.Close()

	for ; iter.Valid(); iter.Next() {
		post, err := iter.Value()
		if err != nil {
			continue
		}
		changed := false
		for i := 0; i < len(post.Tags); i++ {
			if post.Tags[i] == tagName {
				post.Tags = append(post.Tags[:i], post.Tags[i+1:]...)
				changed = true
				break
			}
		}
		if changed {
			_ = k.Post.Set(ctx, post.PostId, post)
		}
	}
	return nil
}

// GetPostAuthor returns the author address for the given post.
func (k Keeper) GetPostAuthor(ctx context.Context, postID uint64) (string, error) {
	post, err := k.Post.Get(ctx, postID)
	if err != nil {
		return "", fmt.Errorf("post %d not found: %w", postID, err)
	}
	return post.Author, nil
}

// GetPostTags returns the tag list for the given post.
func (k Keeper) GetPostTags(ctx context.Context, postID uint64) ([]string, error) {
	post, err := k.Post.Get(ctx, postID)
	if err != nil {
		return nil, fmt.Errorf("post %d not found: %w", postID, err)
	}
	return post.Tags, nil
}

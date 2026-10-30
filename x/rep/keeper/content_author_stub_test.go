package keeper_test

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/rep/keeper"
)

// anyContentAuthor answers every blog post, blog reply and collection lookup
// with one fixed author, so a content stake in a test resolves to an existing
// item owned by someone other than the staker (CreateStake requires the
// content to exist, REP-E-18).
type anyContentAuthor struct{ author string }

func (a anyContentAuthor) GetPostAuthor(context.Context, uint64) (string, error) {
	return a.author, nil
}
func (a anyContentAuthor) GetReplyAuthor(context.Context, uint64) (string, error) {
	return a.author, nil
}
func (a anyContentAuthor) GetCollectionOwner(context.Context, uint64) (string, error) {
	return a.author, nil
}

// wireContentAuthor wires blog, forum and collect stubs that report a fixed
// third-party author for every content id.
func wireContentAuthor(k keeper.Keeper) {
	author := sdk.AccAddress([]byte("content_author______")).String()
	k.SetBlogKeeper(anyContentAuthor{author: author})
	k.SetCollectKeeper(anyContentAuthor{author: author})
	k.SetForumKeeper(&mockForumKeeper{anyAuthor: author})
}

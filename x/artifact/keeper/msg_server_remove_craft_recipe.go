package keeper

import (
	"context"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/artifact/types"
)

// RemoveCraftRecipe removes a recipe (owner only, at any time).
func (k msgServer) RemoveCraftRecipe(ctx context.Context, msg *types.MsgRemoveCraftRecipe) (*types.MsgRemoveCraftRecipeResponse, error) {
	if _, err := k.decodeAddr("owner", msg.Owner); err != nil {
		return nil, err
	}
	c, err := k.getOwnedClass(ctx, msg.ClassId, msg.Owner)
	if err != nil {
		return nil, err
	}
	key := collections.Join(c.Id, msg.RecipeId)
	if has, err := k.CraftRecipes.Has(ctx, key); err != nil {
		return nil, err
	} else if !has {
		return nil, types.ErrRecipeNotFound
	}
	if err := k.CraftRecipes.Remove(ctx, key); err != nil {
		return nil, err
	}
	emit(ctx, types.EventCraftRecipeRemoved, classAttr(c.Id), sdk.NewAttribute("recipe_id", u64(msg.RecipeId)))
	return &types.MsgRemoveCraftRecipeResponse{}, nil
}

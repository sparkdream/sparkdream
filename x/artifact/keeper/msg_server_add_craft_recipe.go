package keeper

import (
	"context"

	"cosmossdk.io/collections"
	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/artifact/types"
)

// AddCraftRecipe adds a recipe to a class (owner only), until the class's
// first craft (docs/x-artifact-spec.md §5.12.1).
func (k msgServer) AddCraftRecipe(ctx context.Context, msg *types.MsgAddCraftRecipe) (*types.MsgAddCraftRecipeResponse, error) {
	if _, err := k.decodeAddr("owner", msg.Owner); err != nil {
		return nil, err
	}
	if msg.AcceptedContentLicense != types.ContentLicense {
		return nil, errorsmod.Wrapf(types.ErrContentLicenseNotAccepted, "accepted_content_license must be %q", types.ContentLicense)
	}
	p := k.GetParams(ctx)
	c, err := k.getOwnedClass(ctx, msg.ClassId, msg.Owner)
	if err != nil {
		return nil, err
	}
	if c.Status == types.ContentStatus_CONTENT_STATUS_HIDDEN {
		return nil, types.ErrContentHidden
	}
	if c.CraftingLocked {
		return nil, types.ErrRecipesLocked
	}
	var count uint32
	if err := k.CraftRecipes.Walk(ctx, collections.NewPrefixedPairRange[uint64, uint64](c.Id), func(U64Pair, types.CraftRecipe) (bool, error) {
		count++
		return false, nil
	}); err != nil {
		return nil, err
	}
	if count >= p.MaxCraftRecipesPerClass {
		return nil, errorsmod.Wrapf(types.ErrInvalidRecipe, "at most %d recipes per class", p.MaxCraftRecipesPerClass)
	}
	if err := k.validateCraftInputs(ctx, msg.Inputs, p); err != nil {
		return nil, err
	}
	quantity := msg.OutputQuantity
	if !c.IsEditions() {
		quantity = 1
	}
	if err := k.validateCraftOutput(ctx, c, msg.OutputMetadata, msg.OutputEditionId, quantity, p); err != nil {
		return nil, err
	}
	c.NextRecipeId++
	r := types.CraftRecipe{
		ClassId:         c.Id,
		Id:              c.NextRecipeId,
		Inputs:          msg.Inputs,
		OutputMetadata:  msg.OutputMetadata,
		OutputEditionId: msg.OutputEditionId,
		OutputQuantity:  quantity,
	}
	if err := k.CraftRecipes.Set(ctx, collections.Join(c.Id, r.Id), r); err != nil {
		return nil, err
	}
	if err := k.saveClass(ctx, c); err != nil {
		return nil, err
	}
	emit(ctx, types.EventCraftRecipeAdded, classAttr(c.Id), sdk.NewAttribute("recipe_id", u64(r.Id)),
		sdk.NewAttribute("inputs", u64(uint64(len(r.Inputs)))))
	return &types.MsgAddCraftRecipeResponse{RecipeId: r.Id}, nil
}

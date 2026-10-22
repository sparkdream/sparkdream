package keeper

import (
	"context"

	"cosmossdk.io/collections"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"sparkdream/x/artifact/types"
)

func (q queryServer) CraftRecipes(ctx context.Context, req *types.QueryCraftRecipesRequest) (*types.QueryCraftRecipesResponse, error) {
	if req == nil {
		return nil, errInvalidRequest
	}
	c, err := q.k.Classes.Get(ctx, req.ClassId)
	if err != nil {
		return nil, notFound(err, "class")
	}
	resp := &types.QueryCraftRecipesResponse{Locked: c.CraftingLocked}
	err = q.k.CraftRecipes.Walk(ctx, collections.NewPrefixedPairRange[uint64, uint64](req.ClassId),
		func(_ U64Pair, r types.CraftRecipe) (bool, error) {
			resp.Recipes = append(resp.Recipes, r)
			return false, nil
		})
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return resp, nil
}

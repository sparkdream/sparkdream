package keeper

import (
	"context"
	"errors"

	"sparkdream/x/commons/types"

	"cosmossdk.io/collections"
)

// GetCategory returns the category with the given id and whether it exists.
func (k Keeper) GetCategory(ctx context.Context, id uint64) (types.Category, bool) {
	cat, err := k.Category.Get(ctx, id)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return types.Category{}, false
		}
		return types.Category{}, false
	}
	return cat, true
}

// HasCategory reports whether a category with the given id exists.
func (k Keeper) HasCategory(ctx context.Context, id uint64) bool {
	has, _ := k.Category.Has(ctx, id)
	return has
}

package keeper

import (
	"context"
	"errors"

	"sparkdream/x/commons/types"

	"cosmossdk.io/collections"
)

// GetParams gets the module parameters from the collections store.
// If no params exist, it returns the default parameters.
func (k Keeper) GetParams(ctx context.Context) (types.Params, error) {
	params, err := k.Params.Get(ctx)
	if err != nil {
		if errors.Is(err, collections.ErrNotFound) {
			return types.DefaultParams(), nil
		}
		return types.Params{}, err
	}

	return params, nil
}

// SetParams sets the module parameters to the collections store.
func (k Keeper) SetParams(ctx context.Context, params types.Params) error {
	if err := params.Validate(); err != nil {
		return err
	}
	return k.Params.Set(ctx, params)
}

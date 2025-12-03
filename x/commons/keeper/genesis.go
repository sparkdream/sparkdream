package keeper

import (
	"context"

	"sparkdream/x/commons/types"
)

// InitGenesis initializes the module's state from a provided genesis state.
func (k Keeper) InitGenesis(ctx context.Context, genState types.GenesisState) error {
	// 1. Set Params
	if err := k.Params.Set(ctx, genState.Params); err != nil {
		panic(err)
	}

	// 2. Bootstrap the Commons Council group
	k.BootstrapCommonsCouncil(ctx)
	for _, elem := range genState.PolicyPermissionsMap {
		if err := k.PolicyPermissions.Set(ctx, elem.PolicyAddress, elem); err != nil {
			return err
		}
	}

	return nil
}

// ExportGenesis returns the module's exported genesis.
func (k Keeper) ExportGenesis(ctx context.Context) (*types.GenesisState, error) {
	var err error

	genesis := types.DefaultGenesis()
	genesis.Params, err = k.Params.Get(ctx)
	if err != nil {
		return nil, err
	}
	if err := k.PolicyPermissions.Walk(ctx, nil, func(_ string, val types.PolicyPermissions) (stop bool, err error) {
		genesis.PolicyPermissionsMap = append(genesis.PolicyPermissionsMap, val)
		return false, nil
	}); err != nil {
		return nil, err
	}

	return genesis, nil
}

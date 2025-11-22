package keeper

import (
	"context"

	"sparkdream/x/name/types"

	"cosmossdk.io/collections"
)

// InitGenesis initializes the module's state from a provided genesis state.
func (k Keeper) InitGenesis(ctx context.Context, genState types.GenesisState) error {
	// 1. Set Params
	if err := k.Params.Set(ctx, genState.Params); err != nil {
		return err
	}

	// 2. Set Name Records and Rebuild Secondary Index
	for _, elem := range genState.NameRecords {
		if err := k.Names.Set(ctx, elem.Name, elem); err != nil {
			return err
		}
		// Rebuild the Owner -> Name index
		if err := k.OwnerNames.Set(ctx, collections.Join(elem.Owner, elem.Name)); err != nil {
			return err
		}
	}

	// 3. Set Owner Infos
	for _, elem := range genState.OwnerInfos {
		if err := k.Owners.Set(ctx, elem.Address, elem); err != nil {
			return err
		}
	}

	// 4. Set Disputes
	for _, elem := range genState.DisputeMap {
		if err := k.Disputes.Set(ctx, elem.Name, elem); err != nil {
			return err
		}
	}

	return nil
}

// ExportGenesis returns the module's exported genesis.
func (k Keeper) ExportGenesis(ctx context.Context) (*types.GenesisState, error) {
	var err error
	genesis := types.DefaultGenesis()

	// 1. Export Params
	genesis.Params, err = k.Params.Get(ctx)
	if err != nil {
		return nil, err
	}

	// 2. Export Names
	if err := k.Names.Walk(ctx, nil, func(_ string, val types.NameRecord) (stop bool, err error) {
		genesis.NameRecords = append(genesis.NameRecords, val)
		return false, nil
	}); err != nil {
		return nil, err
	}

	// 3. Export Owners
	if err := k.Owners.Walk(ctx, nil, func(_ string, val types.OwnerInfo) (stop bool, err error) {
		genesis.OwnerInfos = append(genesis.OwnerInfos, val)
		return false, nil
	}); err != nil {
		return nil, err
	}

	// 4. Export Disputes
	if err := k.Disputes.Walk(ctx, nil, func(_ string, val types.Dispute) (stop bool, err error) {
		genesis.DisputeMap = append(genesis.DisputeMap, val)
		return false, nil
	}); err != nil {
		return nil, err
	}

	return genesis, nil
}

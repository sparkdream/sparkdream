package keeper

import (
	"context"

	"cosmossdk.io/collections"

	"sparkdream/x/futarchy/types"
)

// InitGenesis initializes the module's state from a provided genesis state.
func (k Keeper) InitGenesis(ctx context.Context, genState types.GenesisState) error {
	var maxIndex uint64
	for _, elem := range genState.MarketMap {
		if err := k.Market.Set(ctx, elem.Index, elem); err != nil {
			return err
		}
		// Rebuild the ActiveMarkets index for every ACTIVE market so the
		// EndBlocker resolution loop can find them after import.
		if elem.Status == "ACTIVE" {
			if err := k.ActiveMarkets.Set(ctx, collections.Join(elem.EndBlock, elem.Index)); err != nil {
				return err
			}
		}
		if elem.Index > maxIndex {
			maxIndex = elem.Index
		}
	}

	// Seed MarketSeq past the max imported index so freshly-created markets
	// never collide with imported IDs.
	if len(genState.MarketMap) > 0 {
		if err := k.MarketSeq.Set(ctx, maxIndex+1); err != nil {
			return err
		}
	}

	return k.Params.Set(ctx, genState.Params)
}

// ExportGenesis returns the module's exported genesis.
func (k Keeper) ExportGenesis(ctx context.Context) (*types.GenesisState, error) {
	var err error

	genesis := types.DefaultGenesis()
	genesis.Params, err = k.Params.Get(ctx)
	if err != nil {
		return nil, err
	}
	if err := k.Market.Walk(ctx, nil, func(_ uint64, val types.Market) (stop bool, err error) {
		genesis.MarketMap = append(genesis.MarketMap, val)
		return false, nil
	}); err != nil {
		return nil, err
	}

	return genesis, nil
}

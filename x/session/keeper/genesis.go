package keeper

import (
	"context"

	"cosmossdk.io/collections"

	"sparkdream/x/session/types"
)

// InitGenesis initializes the module's state from a provided genesis state.
func (k Keeper) InitGenesis(ctx context.Context, genState types.GenesisState) error {
	if err := k.Params.Set(ctx, genState.Params); err != nil {
		return err
	}

	for _, session := range genState.Sessions {
		key := collections.Join(session.Granter, session.Grantee)
		if err := k.Sessions.Set(ctx, key, session); err != nil {
			return err
		}
		if err := k.SessionsByGranter.Set(ctx, collections.Join(session.Granter, session.Grantee)); err != nil {
			return err
		}
		if err := k.SessionsByGrantee.Set(ctx, collections.Join(session.Grantee, session.Granter)); err != nil {
			return err
		}
		if err := k.SessionsByExpiration.Set(ctx, collections.Join3(session.Expiration.Unix(), session.Granter, session.Grantee)); err != nil {
			return err
		}
	}

	return nil
}

// ExportGenesis returns the module's exported genesis.
func (k Keeper) ExportGenesis(ctx context.Context) (*types.GenesisState, error) {
	genesis := types.DefaultGenesis()

	var err error
	genesis.Params, err = k.Params.Get(ctx)
	if err != nil {
		return nil, err
	}

	// Export all sessions
	err = k.Sessions.Walk(ctx, nil, func(_ collections.Pair[string, string], session types.Session) (bool, error) {
		genesis.Sessions = append(genesis.Sessions, session)
		return false, nil
	})
	if err != nil {
		return nil, err
	}

	return genesis, nil
}

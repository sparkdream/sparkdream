package keeper

import (
	"context"
	"fmt"

	"cosmossdk.io/collections"

	"sparkdream/x/session/types"
)

// InitGenesis initializes the module's state from a provided genesis state.
//
// Writes each Grant into the primary store and rebuilds every secondary
// index from the grants list (per the plan, indexes are not exported, only
// reconstructed). Restores GrantSeq and reseeds ActiveGrantCountByType
// either from the exported snapshot (when present) or by re-counting
// active grants.
func (k Keeper) InitGenesis(ctx context.Context, genState types.GenesisState) error {
	if err := k.Params.Set(ctx, genState.Params); err != nil {
		return err
	}

	// Track seen grant IDs to validate against GrantSeq.
	var maxID uint64
	activeCounts := make(map[string]uint32) // key = granter + "|" + GrantType as string

	for _, g := range genState.Grants {
		if g.Id == 0 {
			return fmt.Errorf("genesis grant has zero id")
		}
		if err := k.writeGrantNoCount(ctx, g); err != nil {
			return err
		}
		if g.Id > maxID {
			maxID = g.Id
		}
		if activeStatus(g.Status) {
			key := fmt.Sprintf("%s|%d", g.Granter, int32(g.Type))
			activeCounts[key]++
		}
	}

	// Seed GrantSeq: prefer the exported value, falling back to the max
	// observed grant id + 1.
	seqValue := genState.GrantSeq
	if seqValue == 0 && maxID > 0 {
		seqValue = maxID + 1
	}
	if seqValue > 0 {
		if err := k.GrantSeq.Set(ctx, seqValue); err != nil {
			return err
		}
	}

	// Seed ActiveGrantCountByType. Prefer the exported snapshot when
	// non-empty (so the count and the grant list cross-validate); fall
	// back to recomputing.
	if len(genState.ActiveGrantCounts) > 0 {
		for _, c := range genState.ActiveGrantCounts {
			if c.Count == 0 {
				continue
			}
			if err := k.ActiveGrantCountByType.Set(
				ctx,
				collections.Join(c.Granter, int32(c.Type)),
				c.Count,
			); err != nil {
				return err
			}
		}
	} else {
		for _, g := range genState.Grants {
			if !activeStatus(g.Status) {
				continue
			}
			if err := k.incActiveGrantCount(ctx, g.Granter, g.Type); err != nil {
				return err
			}
		}
	}

	return nil
}

// writeGrantNoCount persists a grant and all secondary indexes without
// adjusting ActiveGrantCountByType. Used at genesis init since
// ActiveGrantCountByType is either restored from the snapshot or
// recomputed once at the end, not incremented per-grant via the normal
// writeGrant path.
func (k Keeper) writeGrantNoCount(ctx context.Context, g types.Grant) error {
	if err := k.Grants.Set(ctx, g.Id, g); err != nil {
		return err
	}
	if err := k.GrantsByGranter.Set(ctx, collections.Join(g.Granter, g.Id)); err != nil {
		return err
	}
	if err := k.GrantsByGrantee.Set(ctx, collections.Join(g.Grantee, g.Id)); err != nil {
		return err
	}
	if err := k.GrantsByExpiration.Set(ctx, collections.Join(g.ExpiresAt.Unix(), g.Id)); err != nil {
		return err
	}
	if err := k.GrantsByTypeAndGranter.Set(ctx, collections.Join3(int32(g.Type), g.Granter, g.Id)); err != nil {
		return err
	}
	if g.Type == types.GrantType_GRANT_TYPE_SESSION_KEY {
		if err := k.SessionKeyByPair.Set(ctx, collections.Join(g.Granter, g.Grantee), g.Id); err != nil {
			return err
		}
	}
	return nil
}

// ExportGenesis returns the module's exported genesis state.
//
// Exports the grants list, GrantSeq, and per-(granter, type) active
// counts; secondary indexes are rebuilt from the grants list on InitGenesis
// rather than exported.
func (k Keeper) ExportGenesis(ctx context.Context) (*types.GenesisState, error) {
	genesis := types.DefaultGenesis()

	var err error
	genesis.Params, err = k.Params.Get(ctx)
	if err != nil {
		return nil, err
	}

	err = k.Grants.Walk(ctx, nil, func(_ uint64, g types.Grant) (bool, error) {
		genesis.Grants = append(genesis.Grants, g)
		return false, nil
	})
	if err != nil {
		return nil, err
	}

	genesis.GrantSeq, err = k.GrantSeq.Peek(ctx)
	if err != nil {
		return nil, err
	}

	err = k.ActiveGrantCountByType.Walk(ctx, nil, func(key collections.Pair[string, int32], count uint32) (bool, error) {
		genesis.ActiveGrantCounts = append(genesis.ActiveGrantCounts, types.ActiveGrantCount{
			Granter: key.K1(),
			Type:    types.GrantType(key.K2()),
			Count:   count,
		})
		return false, nil
	})
	if err != nil {
		return nil, err
	}

	return genesis, nil
}

package keeper

import (
	"context"
	"fmt"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"

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
	// observed grant id (nextGrantID returns sequence+1, so a sequence equal
	// to the highest id allocates the next free id).
	seqValue := genState.GrantSeq
	if seqValue < maxID {
		seqValue = maxID
	}
	if seqValue > 0 {
		if err := k.GrantSeq.Set(ctx, seqValue); err != nil {
			return err
		}
	}

	// Seed ActiveGrantCountByType from the grants themselves. The exported
	// counts are checked against them in GenesisState.Validate (SES-19) and
	// never trusted on their own: an inflated count would refuse new grants,
	// an understated one would bypass the per-granter caps.
	for _, g := range genState.Grants {
		if g.Status == types.GrantStatus_GRANT_STATUS_PENDING {
			// Per-granter pending counts are never exported: recomputed.
			if err := k.incPendingGrantCount(ctx, g.Granter); err != nil {
				return err
			}
			continue
		}
		if !activeStatus(g.Status) {
			continue
		}
		if err := k.incActiveGrantCount(ctx, g.Granter, g.Type); err != nil {
			return err
		}
	}

	// Escrowed oneshot deposits, per-day spend buckets and bypass
	// creators (SES-11): without them an import strands deposits in the
	// module account and resets the per-day pull throttle.
	for _, d := range genState.OneshotGasDeposits {
		if err := k.OneshotGasDeposit.Set(ctx, d.GrantId, d.Amount); err != nil {
			return err
		}
	}
	for _, e := range genState.EpochSpends {
		if err := k.EpochSpendByGrant.Set(ctx, collections.Join(e.GrantId, e.Day), e.Amount); err != nil {
			return err
		}
	}
	for _, c := range genState.GrantCreators {
		if err := k.GrantCreatorModule.Set(ctx, c.GrantId, c.ModuleAddress); err != nil {
			return err
		}
	}
	// Grantee consent (SES-102): allowed granters (both directions) and the
	// deposits escrowed for pending grants.
	for _, a := range genState.AllowedGranters {
		if err := k.setAllowedGranter(ctx, a.Grantee, a.Granter); err != nil {
			return err
		}
	}
	for _, d := range genState.PendingGrantDeposits {
		if err := k.PendingGrantDeposit.Set(ctx, d.GrantId, d.Amount); err != nil {
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

	err = k.OneshotGasDeposit.Walk(ctx, nil, func(id uint64, c sdk.Coin) (bool, error) {
		genesis.OneshotGasDeposits = append(genesis.OneshotGasDeposits, types.OneshotDeposit{GrantId: id, Amount: c})
		return false, nil
	})
	if err != nil {
		return nil, err
	}
	err = k.EpochSpendByGrant.Walk(ctx, nil, func(key collections.Pair[uint64, int64], v string) (bool, error) {
		genesis.EpochSpends = append(genesis.EpochSpends, types.EpochSpendEntry{GrantId: key.K1(), Day: key.K2(), Amount: v})
		return false, nil
	})
	if err != nil {
		return nil, err
	}
	err = k.GrantCreatorModule.Walk(ctx, nil, func(id uint64, addr string) (bool, error) {
		genesis.GrantCreators = append(genesis.GrantCreators, types.GrantCreator{GrantId: id, ModuleAddress: addr})
		return false, nil
	})
	if err != nil {
		return nil, err
	}
	err = k.AllowedGranters.Walk(ctx, nil, func(key collections.Pair[string, string]) (bool, error) {
		genesis.AllowedGranters = append(genesis.AllowedGranters, types.AllowedGranter{Grantee: key.K1(), Granter: key.K2()})
		return false, nil
	})
	if err != nil {
		return nil, err
	}
	err = k.PendingGrantDeposit.Walk(ctx, nil, func(id uint64, c sdk.Coin) (bool, error) {
		genesis.PendingGrantDeposits = append(genesis.PendingGrantDeposits, types.PendingGrantDeposit{GrantId: id, Amount: c})
		return false, nil
	})
	if err != nil {
		return nil, err
	}

	return genesis, nil
}

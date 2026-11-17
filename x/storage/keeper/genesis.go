package keeper

import (
	"context"
	"fmt"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"

	"sparkdream/x/storage/types"
)

// InitGenesis initializes the module's state from a provided genesis state.
// Indexes and timers are rebuilt from the requests, not trusted from the file.
func (k Keeper) InitGenesis(ctx context.Context, genState types.GenesisState) error {
	p := genState.Params
	if err := k.Params.Set(ctx, p); err != nil {
		return err
	}
	for _, b := range genState.Backends {
		if err := k.Backends.Set(ctx, b.Name, b); err != nil {
			return err
		}
		// Reads x/service, so it cannot be in Validate. Skipped when the
		// service type is not there yet (a chain that seeds it later).
		if k.late.service != nil {
			if _, ok := k.late.service.GetServiceTypeConfig(ctx, b.ServiceType); ok {
				if err := k.checkUnbondingCovers(ctx, p, b.ServiceType); err != nil {
					return err
				}
			}
		}
	}
	for _, a := range genState.ApprovedOperators {
		if err := k.ApprovedOperators.Set(ctx, collections.Join(a.Backend, a.Operator),
			types.Approval{Controller: a.Controller, Height: a.Height}); err != nil {
			return err
		}
	}
	for _, a := range genState.ApprovedScanners {
		if err := k.ApprovedScanners.Set(ctx, a.Scanner, types.Approval{Controller: a.Controller, Height: a.Height}); err != nil {
			return err
		}
	}
	escrow := math.ZeroInt()
	for _, r := range genState.Requests {
		if err := k.saveRequest(ctx, nil, r, p); err != nil {
			return err
		}
		escrow = escrow.Add(r.CopyFee)
	}
	for _, rec := range genState.RemovedHashes {
		if err := k.RemovedHashes.Set(ctx, rec.Sha256, rec); err != nil {
			return err
		}
	}
	for _, c := range genState.Counters {
		if err := k.Counters.Set(ctx, collections.Join(c.Address, int32(c.Kind)),
			types.Counter{WindowStart: c.WindowStart, Count: c.Count}); err != nil {
			return err
		}
	}
	if err := k.RequestSeq.Set(ctx, genState.RequestSeq); err != nil {
		return err
	}
	// "At least", never "equal": a coin that reached the account by some
	// path the blocked list does not cover must not stop a chain starting.
	if escrow.IsPositive() && k.late.identity != nil {
		have := k.bankKeeper.GetBalance(ctx, k.moduleAddress(), k.BondDenom(ctx)).Amount
		if have.LT(escrow) {
			return fmt.Errorf("storage module account holds %s, requests need %s in escrow", have, escrow)
		}
	}
	return nil
}

// ExportGenesis returns the module's exported genesis.
func (k Keeper) ExportGenesis(ctx context.Context) (*types.GenesisState, error) {
	var err error
	genesis := types.DefaultGenesis()
	genesis.Backends = nil
	genesis.Params, err = k.Params.Get(ctx)
	if err != nil {
		return nil, err
	}
	if err := k.Backends.Walk(ctx, nil, func(_ string, b types.Backend) (bool, error) {
		genesis.Backends = append(genesis.Backends, b)
		return false, nil
	}); err != nil {
		return nil, err
	}
	if err := k.ApprovedOperators.Walk(ctx, nil, func(key collections.Pair[string, string], a types.Approval) (bool, error) {
		genesis.ApprovedOperators = append(genesis.ApprovedOperators, types.ApprovedOperator{
			Backend: key.K1(), Operator: key.K2(), Controller: a.Controller, Height: a.Height})
		return false, nil
	}); err != nil {
		return nil, err
	}
	if err := k.ApprovedScanners.Walk(ctx, nil, func(scanner string, a types.Approval) (bool, error) {
		genesis.ApprovedScanners = append(genesis.ApprovedScanners, types.ApprovedScanner{
			Scanner: scanner, Controller: a.Controller, Height: a.Height})
		return false, nil
	}); err != nil {
		return nil, err
	}
	if err := k.Requests.Walk(ctx, nil, func(_ uint64, r types.Request) (bool, error) {
		genesis.Requests = append(genesis.Requests, r)
		return false, nil
	}); err != nil {
		return nil, err
	}
	if err := k.RemovedHashes.Walk(ctx, nil, func(_ string, rec types.RemovedHash) (bool, error) {
		genesis.RemovedHashes = append(genesis.RemovedHashes, rec)
		return false, nil
	}); err != nil {
		return nil, err
	}
	if err := k.Counters.Walk(ctx, nil, func(key collections.Pair[string, int32], c types.Counter) (bool, error) {
		genesis.Counters = append(genesis.Counters, types.CounterEntry{
			Address: key.K1(), Kind: types.CounterKind(key.K2()), WindowStart: c.WindowStart, Count: c.Count})
		return false, nil
	}); err != nil {
		return nil, err
	}
	genesis.RequestSeq, err = k.RequestSeq.Peek(ctx)
	if err != nil {
		return nil, err
	}
	return genesis, nil
}

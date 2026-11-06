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
		// Rebuild the Target -> Name reverse-resolution index from the
		// record's own consent flag (same derivation MsgAcceptTarget uses).
		if elem.TargetAccepted && elem.Target != "" {
			if err := k.AcceptedTargets.Set(ctx, collections.Join(elem.Target, elem.Name)); err != nil {
				return err
			}
		}
		if elem.Target != "" {
			if err := k.TargetNames.Set(ctx, collections.Join(elem.Target, elem.Name)); err != nil {
				return err
			}
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
		// SetDispute also rebuilds the PendingDisputes, ReviewDisputes and
		// ClaimantDisputes indexes.
		if err := k.SetDispute(ctx, elem); err != nil {
			return err
		}
	}

	// 5. Set locked stakes (claimant dispute stakes, owner contest stakes)
	for _, elem := range genState.DisputeStakes {
		if err := k.DisputeStakes.Set(ctx, elem.ChallengeId, elem); err != nil {
			return err
		}
	}
	for _, elem := range genState.ContestStakes {
		if err := k.ContestStakes.Set(ctx, elem.ChallengeId, elem); err != nil {
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

	// 5. Export locked stakes
	if err := k.DisputeStakes.Walk(ctx, nil, func(_ string, val types.DisputeStake) (stop bool, err error) {
		genesis.DisputeStakes = append(genesis.DisputeStakes, val)
		return false, nil
	}); err != nil {
		return nil, err
	}
	if err := k.ContestStakes.Walk(ctx, nil, func(_ string, val types.ContestStake) (stop bool, err error) {
		genesis.ContestStakes = append(genesis.ContestStakes, val)
		return false, nil
	}); err != nil {
		return nil, err
	}

	return genesis, nil
}

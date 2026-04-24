package keeper

import (
	"context"
	"strconv"

	"cosmossdk.io/collections"

	"sparkdream/x/collect/types"
)

// InitGenesis initializes the module's state from a provided genesis state.
func (k Keeper) InitGenesis(ctx context.Context, genState types.GenesisState) error {
	if err := k.Params.Set(ctx, genState.Params); err != nil {
		return err
	}

	// Set sequence counters
	if err := k.CollectionSeq.Set(ctx, genState.CollectionCount); err != nil {
		return err
	}
	if err := k.ItemSeq.Set(ctx, genState.ItemCount); err != nil {
		return err
	}
	if err := k.CurationReviewSeq.Set(ctx, genState.CurationReviewCount); err != nil {
		return err
	}
	if err := k.HideRecordSeq.Set(ctx, genState.HideRecordCount); err != nil {
		return err
	}

	// Import collections + rebuild secondary indexes
	for _, coll := range genState.Collections {
		if err := k.Collection.Set(ctx, coll.Id, coll); err != nil {
			return err
		}
		if err := k.CollectionsByOwner.Set(ctx, collections.Join(coll.Owner, coll.Id)); err != nil {
			return err
		}
		if coll.ExpiresAt > 0 {
			if err := k.CollectionsByExpiry.Set(ctx, collections.Join(coll.ExpiresAt, coll.Id)); err != nil {
				return err
			}
		}
		if err := k.CollectionsByStatus.Set(ctx, collections.Join(int32(coll.Status), coll.Id)); err != nil {
			return err
		}
		for _, tag := range coll.Tags {
			if err := k.CollectionsByTag.Set(ctx, collections.Join(tag, coll.Id)); err != nil {
				return err
			}
		}
	}

	// Import items + rebuild secondary indexes
	for _, item := range genState.Items {
		if err := k.Item.Set(ctx, item.Id, item); err != nil {
			return err
		}
		if err := k.ItemsByCollection.Set(ctx, collections.Join(item.CollectionId, item.Id)); err != nil {
			return err
		}
		// Look up owner from parent collection for owner index
		coll, err := k.Collection.Get(ctx, item.CollectionId)
		if err == nil {
			if err := k.ItemsByOwner.Set(ctx, collections.Join(coll.Owner, item.Id)); err != nil {
				return err
			}
		}
	}

	// Import collaborators + rebuild reverse index
	for _, collab := range genState.Collaborators {
		key := CollaboratorCompositeKey(collab.CollectionId, collab.Address)
		if err := k.Collaborator.Set(ctx, key, collab); err != nil {
			return err
		}
		if err := k.CollaboratorReverse.Set(ctx, collections.Join(collab.Address, collab.CollectionId)); err != nil {
			return err
		}
	}

	// Import curator activity (collect-specific counters only — generic bond
	// state lives in x/rep BondedRole records).
	for _, activity := range genState.CuratorActivities {
		if err := k.CuratorActivity.Set(ctx, activity.Address, activity); err != nil {
			return err
		}
	}

	// Import curation reviews + rebuild indexes
	for _, review := range genState.CurationReviews {
		if err := k.CurationReview.Set(ctx, review.Id, review); err != nil {
			return err
		}
		if err := k.CurationReviewsByCollection.Set(ctx, collections.Join(review.CollectionId, review.Id)); err != nil {
			return err
		}
		if err := k.CurationReviewsByCurator.Set(ctx, collections.Join(review.Curator, review.Id)); err != nil {
			return err
		}
	}

	// Import curation summaries
	for _, summary := range genState.CurationSummaries {
		if err := k.CurationSummary.Set(ctx, summary.CollectionId, summary); err != nil {
			return err
		}
	}

	// Import sponsorship requests + rebuild expiry index
	for _, req := range genState.SponsorshipRequests {
		if err := k.SponsorshipRequest.Set(ctx, req.CollectionId, req); err != nil {
			return err
		}
		if err := k.SponsorshipRequestsByExpiry.Set(ctx, collections.Join(req.ExpiresAt, req.CollectionId)); err != nil {
			return err
		}
	}

	// Import flags + rebuild indexes
	for _, flag := range genState.Flags {
		key := FlagCompositeKey(flag.TargetType, flag.TargetId)
		if err := k.Flag.Set(ctx, key, flag); err != nil {
			return err
		}
		if flag.InReviewQueue {
			if err := k.FlagReviewQueue.Set(ctx, collections.Join(int32(flag.TargetType), flag.TargetId)); err != nil {
				return err
			}
		}
		if flag.LastFlagAt > 0 {
			if err := k.FlagExpiry.Set(ctx, collections.Join(flag.LastFlagAt+genState.Params.FlagExpirationBlocks, key)); err != nil {
				return err
			}
		}
	}

	// Import hide records + rebuild indexes
	for _, hr := range genState.HideRecords {
		if err := k.HideRecord.Set(ctx, hr.Id, hr); err != nil {
			return err
		}
		targetKey := HideRecordTargetCompositeKey(hr.TargetType, hr.TargetId)
		if err := k.HideRecordByTarget.Set(ctx, collections.Join(targetKey, hr.Id)); err != nil {
			return err
		}
		if !hr.Resolved {
			if err := k.HideRecordExpiry.Set(ctx, collections.Join(hr.AppealDeadline, hr.Id)); err != nil {
				return err
			}
		}
	}

	// Import endorsements + rebuild indexes
	for _, endorsement := range genState.Endorsements {
		if err := k.Endorsement.Set(ctx, endorsement.CollectionId, endorsement); err != nil {
			return err
		}
		if !endorsement.StakeReleased {
			if err := k.EndorsementStakeExpiry.Set(ctx, collections.Join(endorsement.StakeReleaseAt, endorsement.CollectionId)); err != nil {
				return err
			}
		}
	}

	// Rebuild endorsement pending index for PENDING collections
	for _, coll := range genState.Collections {
		if coll.Status == types.CollectionStatus_COLLECTION_STATUS_PENDING && coll.EndorsedBy == "" {
			expiryBlock := coll.CreatedAt + genState.Params.EndorsementExpiryBlocks
			if err := k.EndorsementPending.Set(ctx, collections.Join(expiryBlock, coll.Id)); err != nil {
				return err
			}
		}
	}

	// Write-through the curator bond-role config to x/rep so MsgBondRole
	// enforcement uses collect's seeded values (Phase 3 bonded-role generalization).
	if err := k.SyncCuratorBondedRoleConfig(ctx, genState.Params); err != nil {
		return err
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

	genesis.CollectionCount, err = k.CollectionSeq.Peek(ctx)
	if err != nil {
		return nil, err
	}

	genesis.ItemCount, err = k.ItemSeq.Peek(ctx)
	if err != nil {
		return nil, err
	}

	genesis.CurationReviewCount, err = k.CurationReviewSeq.Peek(ctx)
	if err != nil {
		return nil, err
	}

	genesis.HideRecordCount, err = k.HideRecordSeq.Peek(ctx)
	if err != nil {
		return nil, err
	}

	// Export collections
	err = k.Collection.Walk(ctx, nil, func(id uint64, coll types.Collection) (bool, error) {
		genesis.Collections = append(genesis.Collections, coll)
		return false, nil
	})
	if err != nil {
		return nil, err
	}

	// Export items
	err = k.Item.Walk(ctx, nil, func(id uint64, item types.Item) (bool, error) {
		genesis.Items = append(genesis.Items, item)
		return false, nil
	})
	if err != nil {
		return nil, err
	}

	// Export collaborators
	err = k.Collaborator.Walk(ctx, nil, func(key string, collab types.Collaborator) (bool, error) {
		genesis.Collaborators = append(genesis.Collaborators, collab)
		return false, nil
	})
	if err != nil {
		return nil, err
	}

	// Export curator activity
	err = k.CuratorActivity.Walk(ctx, nil, func(_ string, activity types.CuratorActivity) (bool, error) {
		genesis.CuratorActivities = append(genesis.CuratorActivities, activity)
		return false, nil
	})
	if err != nil {
		return nil, err
	}

	// Export curation reviews
	err = k.CurationReview.Walk(ctx, nil, func(id uint64, review types.CurationReview) (bool, error) {
		genesis.CurationReviews = append(genesis.CurationReviews, review)
		return false, nil
	})
	if err != nil {
		return nil, err
	}

	// Export curation summaries
	err = k.CurationSummary.Walk(ctx, nil, func(id uint64, summary types.CurationSummary) (bool, error) {
		genesis.CurationSummaries = append(genesis.CurationSummaries, summary)
		return false, nil
	})
	if err != nil {
		return nil, err
	}

	// Export sponsorship requests
	err = k.SponsorshipRequest.Walk(ctx, nil, func(id uint64, req types.SponsorshipRequest) (bool, error) {
		genesis.SponsorshipRequests = append(genesis.SponsorshipRequests, req)
		return false, nil
	})
	if err != nil {
		return nil, err
	}

	// Export flags
	err = k.Flag.Walk(ctx, nil, func(key string, flag types.CollectionFlag) (bool, error) {
		genesis.Flags = append(genesis.Flags, flag)
		return false, nil
	})
	if err != nil {
		return nil, err
	}

	// Export hide records
	err = k.HideRecord.Walk(ctx, nil, func(id uint64, hr types.HideRecord) (bool, error) {
		genesis.HideRecords = append(genesis.HideRecords, hr)
		return false, nil
	})
	if err != nil {
		return nil, err
	}

	// Export endorsements
	err = k.Endorsement.Walk(ctx, nil, func(id uint64, endorsement types.Endorsement) (bool, error) {
		genesis.Endorsements = append(genesis.Endorsements, endorsement)
		return false, nil
	})
	if err != nil {
		return nil, err
	}

	return genesis, nil
}

// helper to parse collection ID from collaborator key
func parseCollectionIDFromCollabKey(key string) (uint64, error) {
	for i, c := range key {
		if c == '/' {
			return strconv.ParseUint(key[:i], 10, 64)
		}
	}
	return 0, nil
}

package simulation

import (
	"fmt"
	"math/rand"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"

	"sparkdream/x/collect/keeper"
	"sparkdream/x/collect/types"
)

// --- Find functions ---

// findCollection returns a random collection with the given status, or nil if none exist.
func findCollection(r *rand.Rand, ctx sdk.Context, k keeper.Keeper, status types.CollectionStatus) (*types.Collection, uint64, error) {
	type entry struct {
		id   uint64
		coll types.Collection
	}
	var matches []entry
	err := k.Collection.Walk(ctx, nil, func(id uint64, coll types.Collection) (bool, error) {
		if coll.Status == status {
			matches = append(matches, entry{id, coll})
		}
		return false, nil
	})
	if err != nil || len(matches) == 0 {
		return nil, 0, err
	}
	selected := matches[r.Intn(len(matches))]
	return &selected.coll, selected.id, nil
}

// findCollectionByOwner returns a random collection owned by the given address.
func findCollectionByOwner(r *rand.Rand, ctx sdk.Context, k keeper.Keeper, owner string) (*types.Collection, uint64, error) {
	type entry struct {
		id   uint64
		coll types.Collection
	}
	var matches []entry
	err := k.CollectionsByOwner.Walk(ctx,
		collections.NewPrefixedPairRange[string, uint64](owner),
		func(key collections.Pair[string, uint64]) (bool, error) {
			collID := key.K2()
			coll, err := k.Collection.Get(ctx, collID)
			if err != nil {
				return false, nil
			}
			matches = append(matches, entry{collID, coll})
			return false, nil
		},
	)
	if err != nil || len(matches) == 0 {
		return nil, 0, err
	}
	selected := matches[r.Intn(len(matches))]
	return &selected.coll, selected.id, nil
}

// findMutableCollectionByOwner returns a non-immutable ACTIVE collection owned by the given address.
func findMutableCollectionByOwner(r *rand.Rand, ctx sdk.Context, k keeper.Keeper, owner string) (*types.Collection, uint64, error) {
	type entry struct {
		id   uint64
		coll types.Collection
	}
	var matches []entry
	err := k.CollectionsByOwner.Walk(ctx,
		collections.NewPrefixedPairRange[string, uint64](owner),
		func(key collections.Pair[string, uint64]) (bool, error) {
			collID := key.K2()
			coll, err := k.Collection.Get(ctx, collID)
			if err != nil {
				return false, nil
			}
			if !coll.Immutable && coll.Status == types.CollectionStatus_COLLECTION_STATUS_ACTIVE {
				matches = append(matches, entry{collID, coll})
			}
			return false, nil
		},
	)
	if err != nil || len(matches) == 0 {
		return nil, 0, err
	}
	selected := matches[r.Intn(len(matches))]
	return &selected.coll, selected.id, nil
}

// findItem returns a random item in the given collection.
func findItem(r *rand.Rand, ctx sdk.Context, k keeper.Keeper, collectionID uint64) (*types.Item, uint64, error) {
	type entry struct {
		id   uint64
		item types.Item
	}
	var matches []entry
	err := k.ItemsByCollection.Walk(ctx,
		collections.NewPrefixedPairRange[uint64, uint64](collectionID),
		func(key collections.Pair[uint64, uint64]) (bool, error) {
			itemID := key.K2()
			item, err := k.Item.Get(ctx, itemID)
			if err != nil {
				return false, nil
			}
			matches = append(matches, entry{itemID, item})
			return false, nil
		},
	)
	if err != nil || len(matches) == 0 {
		return nil, 0, err
	}
	selected := matches[r.Intn(len(matches))]
	return &selected.item, selected.id, nil
}

// findItemByOwner returns a random item owned by the given address (via ItemsByOwner index).
func findItemByOwner(r *rand.Rand, ctx sdk.Context, k keeper.Keeper, owner string) (*types.Item, uint64, error) {
	type entry struct {
		id   uint64
		item types.Item
	}
	var matches []entry
	err := k.ItemsByOwner.Walk(ctx,
		collections.NewPrefixedPairRange[string, uint64](owner),
		func(key collections.Pair[string, uint64]) (bool, error) {
			itemID := key.K2()
			item, err := k.Item.Get(ctx, itemID)
			if err != nil {
				return false, nil
			}
			matches = append(matches, entry{itemID, item})
			return false, nil
		},
	)
	if err != nil || len(matches) == 0 {
		return nil, 0, err
	}
	selected := matches[r.Intn(len(matches))]
	return &selected.item, selected.id, nil
}

// findCollaborator returns a random collaborator for the given collection.
func findCollaborator(r *rand.Rand, ctx sdk.Context, k keeper.Keeper, collectionID uint64) (*types.Collaborator, string, error) {
	type entry struct {
		key    string
		collab types.Collaborator
	}
	var matches []entry
	err := k.Collaborator.Walk(ctx, nil, func(key string, collab types.Collaborator) (bool, error) {
		if collab.CollectionId == collectionID {
			matches = append(matches, entry{key, collab})
		}
		return false, nil
	})
	if err != nil || len(matches) == 0 {
		return nil, "", err
	}
	selected := matches[r.Intn(len(matches))]
	return &selected.collab, selected.key, nil
}

// findAnyCollaborator returns a random collaborator from any collection.
func findAnyCollaborator(r *rand.Rand, ctx sdk.Context, k keeper.Keeper) (*types.Collaborator, string, error) {
	type entry struct {
		key    string
		collab types.Collaborator
	}
	var matches []entry
	err := k.Collaborator.Walk(ctx, nil, func(key string, collab types.Collaborator) (bool, error) {
		matches = append(matches, entry{key, collab})
		return false, nil
	})
	if err != nil || len(matches) == 0 {
		return nil, "", err
	}
	selected := matches[r.Intn(len(matches))]
	return &selected.collab, selected.key, nil
}

// findCurator returns a random active curator.
func findCurator(r *rand.Rand, ctx sdk.Context, k keeper.Keeper) (*types.Curator, string, error) {
	type entry struct {
		addr    string
		curator types.Curator
	}
	var matches []entry
	err := k.Curator.Walk(ctx, nil, func(addr string, curator types.Curator) (bool, error) {
		if curator.Active {
			matches = append(matches, entry{addr, curator})
		}
		return false, nil
	})
	if err != nil || len(matches) == 0 {
		return nil, "", err
	}
	selected := matches[r.Intn(len(matches))]
	return &selected.curator, selected.addr, nil
}

// findRemovableCurator returns an active curator with PendingChallenges==0.
func findRemovableCurator(r *rand.Rand, ctx sdk.Context, k keeper.Keeper) (*types.Curator, string, error) {
	type entry struct {
		addr    string
		curator types.Curator
	}
	var matches []entry
	err := k.Curator.Walk(ctx, nil, func(addr string, curator types.Curator) (bool, error) {
		if curator.Active && curator.PendingChallenges == 0 {
			matches = append(matches, entry{addr, curator})
		}
		return false, nil
	})
	if err != nil || len(matches) == 0 {
		return nil, "", err
	}
	selected := matches[r.Intn(len(matches))]
	return &selected.curator, selected.addr, nil
}

// findUnchallengedReview returns a random review that has not been challenged.
func findUnchallengedReview(r *rand.Rand, ctx sdk.Context, k keeper.Keeper) (*types.CurationReview, uint64, error) {
	type entry struct {
		id     uint64
		review types.CurationReview
	}
	var matches []entry
	err := k.CurationReview.Walk(ctx, nil, func(id uint64, review types.CurationReview) (bool, error) {
		if !review.Challenged {
			matches = append(matches, entry{id, review})
		}
		return false, nil
	})
	if err != nil || len(matches) == 0 {
		return nil, 0, err
	}
	selected := matches[r.Intn(len(matches))]
	return &selected.review, selected.id, nil
}

// findSponsorshipRequest returns a random sponsorship request.
func findSponsorshipRequest(r *rand.Rand, ctx sdk.Context, k keeper.Keeper) (*types.SponsorshipRequest, uint64, error) {
	type entry struct {
		collID uint64
		req    types.SponsorshipRequest
	}
	var matches []entry
	err := k.SponsorshipRequest.Walk(ctx, nil, func(collID uint64, req types.SponsorshipRequest) (bool, error) {
		matches = append(matches, entry{collID, req})
		return false, nil
	})
	if err != nil || len(matches) == 0 {
		return nil, 0, err
	}
	selected := matches[r.Intn(len(matches))]
	return &selected.req, selected.collID, nil
}

// findHideRecord returns a random hide record matching the appealed status.
func findHideRecord(r *rand.Rand, ctx sdk.Context, k keeper.Keeper, appealed bool) (*types.HideRecord, uint64, error) {
	type entry struct {
		id uint64
		hr types.HideRecord
	}
	var matches []entry
	err := k.HideRecord.Walk(ctx, nil, func(id uint64, hr types.HideRecord) (bool, error) {
		if hr.Appealed == appealed && !hr.Resolved {
			matches = append(matches, entry{id, hr})
		}
		return false, nil
	})
	if err != nil || len(matches) == 0 {
		return nil, 0, err
	}
	selected := matches[r.Intn(len(matches))]
	return &selected.hr, selected.id, nil
}

// findSeekingEndorsementCollection returns a random PENDING collection that is seeking endorsement.
func findSeekingEndorsementCollection(r *rand.Rand, ctx sdk.Context, k keeper.Keeper) (*types.Collection, uint64, error) {
	type entry struct {
		id   uint64
		coll types.Collection
	}
	var matches []entry
	err := k.Collection.Walk(ctx, nil, func(id uint64, coll types.Collection) (bool, error) {
		if coll.Status == types.CollectionStatus_COLLECTION_STATUS_PENDING && coll.SeekingEndorsement {
			matches = append(matches, entry{id, coll})
		}
		return false, nil
	})
	if err != nil || len(matches) == 0 {
		return nil, 0, err
	}
	selected := matches[r.Intn(len(matches))]
	return &selected.coll, selected.id, nil
}

// --- Get-or-create functions ---

// getOrCreateCollection creates or finds an existing PUBLIC ACTIVE collection for the owner.
func getOrCreateCollection(r *rand.Rand, ctx sdk.Context, k keeper.Keeper, owner string) (uint64, error) {
	coll, collID, _ := findMutableCollectionByOwner(r, ctx, k, owner)
	if coll != nil {
		return collID, nil
	}

	collID, err := k.CollectionSeq.Next(ctx)
	if err != nil {
		return 0, err
	}

	now := ctx.BlockHeight()
	newColl := types.Collection{
		Id:                       collID,
		Owner:                    owner,
		Name:                     randomCollectionName(r),
		Description:              randomDescription(r),
		Tags:                     []string{randomTag(r), randomTag(r)},
		Type:                     randomCollectionType(r),
		Visibility:               types.Visibility_VISIBILITY_PUBLIC,
		Status:                   types.CollectionStatus_COLLECTION_STATUS_ACTIVE,
		CreatedAt:                now,
		UpdatedAt:                now,
		DepositAmount:            math.ZeroInt(),
		ItemDepositTotal:         math.ZeroInt(),
		CommunityFeedbackEnabled: true,
		Immutable:                false,
	}

	if err := k.Collection.Set(ctx, collID, newColl); err != nil {
		return 0, err
	}
	if err := k.CollectionsByOwner.Set(ctx, collections.Join(owner, collID)); err != nil {
		return 0, err
	}
	if err := k.CollectionsByStatus.Set(ctx, collections.Join(int32(newColl.Status), collID)); err != nil {
		return 0, err
	}

	return collID, nil
}

// getOrCreateTTLCollection creates or finds a TTL collection with an expiry.
func getOrCreateTTLCollection(r *rand.Rand, ctx sdk.Context, k keeper.Keeper, owner string) (uint64, error) {
	// Try to find existing TTL collection owned by this owner
	coll, collID, _ := findCollectionByOwner(r, ctx, k, owner)
	if coll != nil && coll.ExpiresAt > 0 {
		return collID, nil
	}

	collID, err := k.CollectionSeq.Next(ctx)
	if err != nil {
		return 0, err
	}

	now := ctx.BlockHeight()
	expiresAt := now + int64(r.Intn(10000)+1000)
	newColl := types.Collection{
		Id:                       collID,
		Owner:                    owner,
		Name:                     randomCollectionName(r),
		Description:              randomDescription(r),
		Tags:                     []string{randomTag(r)},
		Type:                     randomCollectionType(r),
		Visibility:               types.Visibility_VISIBILITY_PUBLIC,
		Status:                   types.CollectionStatus_COLLECTION_STATUS_ACTIVE,
		CreatedAt:                now,
		UpdatedAt:                now,
		ExpiresAt:                expiresAt,
		DepositAmount:            math.NewInt(int64(r.Intn(1000) + 100)),
		ItemDepositTotal:         math.ZeroInt(),
		CommunityFeedbackEnabled: true,
		Immutable:                false,
	}

	if err := k.Collection.Set(ctx, collID, newColl); err != nil {
		return 0, err
	}
	if err := k.CollectionsByOwner.Set(ctx, collections.Join(owner, collID)); err != nil {
		return 0, err
	}
	if err := k.CollectionsByStatus.Set(ctx, collections.Join(int32(newColl.Status), collID)); err != nil {
		return 0, err
	}
	if err := k.CollectionsByExpiry.Set(ctx, collections.Join(expiresAt, collID)); err != nil {
		return 0, err
	}

	return collID, nil
}

// getOrCreateItem creates or finds an existing item in the given collection.
func getOrCreateItem(r *rand.Rand, ctx sdk.Context, k keeper.Keeper, collectionID uint64, owner string) (uint64, error) {
	item, itemID, _ := findItem(r, ctx, k, collectionID)
	if item != nil {
		return itemID, nil
	}

	coll, err := k.Collection.Get(ctx, collectionID)
	if err != nil {
		return 0, err
	}

	itemID, err = k.ItemSeq.Next(ctx)
	if err != nil {
		return 0, err
	}

	now := ctx.BlockHeight()
	newItem := types.Item{
		Id:            itemID,
		CollectionId:  collectionID,
		AddedBy:       owner,
		Title:         randomTitle(r),
		Description:   randomDescription(r),
		ReferenceType: types.ReferenceType_REFERENCE_TYPE_LINK,
		Link:          &types.LinkReference{Uri: randomURI(r)},
		Position:      coll.ItemCount,
		AddedAt:       now,
		Status:        types.ItemStatus_ITEM_STATUS_ACTIVE,
	}

	if err := k.Item.Set(ctx, itemID, newItem); err != nil {
		return 0, err
	}
	if err := k.ItemsByCollection.Set(ctx, collections.Join(collectionID, itemID)); err != nil {
		return 0, err
	}
	if err := k.ItemsByOwner.Set(ctx, collections.Join(owner, itemID)); err != nil {
		return 0, err
	}

	coll.ItemCount++
	coll.UpdatedAt = now
	if err := k.Collection.Set(ctx, collectionID, coll); err != nil {
		return 0, err
	}

	return itemID, nil
}

// getOrCreateCollaborator creates a collaborator if one doesn't exist for the collection+address.
func getOrCreateCollaborator(ctx sdk.Context, k keeper.Keeper, collectionID uint64, collabAddr string, role types.CollaboratorRole) error {
	key := keeper.CollaboratorCompositeKey(collectionID, collabAddr)
	_, err := k.Collaborator.Get(ctx, key)
	if err == nil {
		return nil // already exists
	}

	now := ctx.BlockHeight()
	collab := types.Collaborator{
		CollectionId: collectionID,
		Address:      collabAddr,
		Role:         role,
		AddedAt:      now,
	}

	if err := k.Collaborator.Set(ctx, key, collab); err != nil {
		return err
	}
	if err := k.CollaboratorReverse.Set(ctx, collections.Join(collabAddr, collectionID)); err != nil {
		return err
	}

	coll, err := k.Collection.Get(ctx, collectionID)
	if err != nil {
		return err
	}
	coll.CollaboratorCount++
	coll.UpdatedAt = now
	return k.Collection.Set(ctx, collectionID, coll)
}

// getOrCreateCurator creates an active curator record if one doesn't exist.
func getOrCreateCurator(r *rand.Rand, ctx sdk.Context, k keeper.Keeper, curatorAddr string) error {
	_, err := k.Curator.Get(ctx, curatorAddr)
	if err == nil {
		return nil
	}

	curator := types.Curator{
		Address:      curatorAddr,
		BondAmount:   math.NewInt(int64(r.Intn(10000) + 1000)),
		RegisteredAt: ctx.BlockHeight(),
		Active:       true,
	}

	return k.Curator.Set(ctx, curatorAddr, curator)
}

// getOrCreateCurationReview creates a curation review if one doesn't exist for curator+collection.
func getOrCreateCurationReview(r *rand.Rand, ctx sdk.Context, k keeper.Keeper, curatorAddr string, collectionID uint64) (uint64, error) {
	// Check if this curator already reviewed this collection
	var existingID *uint64
	_ = k.CurationReviewsByCollection.Walk(ctx,
		collections.NewPrefixedPairRange[uint64, uint64](collectionID),
		func(key collections.Pair[uint64, uint64]) (bool, error) {
			review, err := k.CurationReview.Get(ctx, key.K2())
			if err == nil && review.Curator == curatorAddr {
				id := key.K2()
				existingID = &id
				return true, nil
			}
			return false, nil
		},
	)
	if existingID != nil {
		return *existingID, nil
	}

	reviewID, err := k.CurationReviewSeq.Next(ctx)
	if err != nil {
		return 0, err
	}

	now := ctx.BlockHeight()
	review := types.CurationReview{
		Id:           reviewID,
		CollectionId: collectionID,
		Curator:      curatorAddr,
		Verdict:      randomCurationVerdict(r),
		Tags:         []string{randomTag(r)},
		Comment:      randomDescription(r),
		CreatedAt:    now,
	}

	if err := k.CurationReview.Set(ctx, reviewID, review); err != nil {
		return 0, err
	}
	if err := k.CurationReviewsByCollection.Set(ctx, collections.Join(collectionID, reviewID)); err != nil {
		return 0, err
	}
	if err := k.CurationReviewsByCurator.Set(ctx, collections.Join(curatorAddr, reviewID)); err != nil {
		return 0, err
	}

	// Update summary
	summary, err := k.CurationSummary.Get(ctx, collectionID)
	if err != nil {
		summary = types.CurationSummary{CollectionId: collectionID}
	}
	if review.Verdict == types.CurationVerdict_CURATION_VERDICT_UP {
		summary.UpCount++
	} else {
		summary.DownCount++
	}
	summary.LastReviewedAt = now
	if err := k.CurationSummary.Set(ctx, collectionID, summary); err != nil {
		return 0, err
	}

	return reviewID, nil
}

// getOrCreateSponsorshipRequest creates a sponsorship request for a collection.
func getOrCreateSponsorshipRequest(r *rand.Rand, ctx sdk.Context, k keeper.Keeper, collectionID uint64, requester string) error {
	_, err := k.SponsorshipRequest.Get(ctx, collectionID)
	if err == nil {
		return nil
	}

	now := ctx.BlockHeight()
	expiresAt := now + int64(r.Intn(10000)+1000)

	req := types.SponsorshipRequest{
		CollectionId:      collectionID,
		Requester:         requester,
		CollectionDeposit: math.NewInt(int64(r.Intn(1000) + 100)),
		ItemDepositTotal:  math.ZeroInt(),
		RequestedAt:       now,
		ExpiresAt:         expiresAt,
	}

	if err := k.SponsorshipRequest.Set(ctx, collectionID, req); err != nil {
		return err
	}
	return k.SponsorshipRequestsByExpiry.Set(ctx, collections.Join(expiresAt, collectionID))
}

// getOrCreateHideRecord creates a hide record for a target.
func getOrCreateHideRecord(r *rand.Rand, ctx sdk.Context, k keeper.Keeper, targetType types.FlagTargetType, targetID uint64, sentinel string) (uint64, error) {
	hr, hrID, _ := findHideRecord(r, ctx, k, false)
	if hr != nil {
		return hrID, nil
	}

	hrID, err := k.HideRecordSeq.Next(ctx)
	if err != nil {
		return 0, err
	}

	now := ctx.BlockHeight()
	appealDeadline := now + int64(r.Intn(10000)+1000)

	record := types.HideRecord{
		Id:              hrID,
		TargetId:        targetID,
		TargetType:      targetType,
		Sentinel:        sentinel,
		HiddenAt:        now,
		CommittedAmount: math.NewInt(int64(r.Intn(500) + 100)),
		ReasonCode:      randomModerationReason(r),
		ReasonText:      "Simulation hide",
		AppealDeadline:  appealDeadline,
	}

	if err := k.HideRecord.Set(ctx, hrID, record); err != nil {
		return 0, err
	}
	targetKey := keeper.HideRecordTargetCompositeKey(targetType, targetID)
	if err := k.HideRecordByTarget.Set(ctx, collections.Join(targetKey, hrID)); err != nil {
		return 0, err
	}
	if err := k.HideRecordExpiry.Set(ctx, collections.Join(appealDeadline, hrID)); err != nil {
		return 0, err
	}

	return hrID, nil
}

// getOrCreateFlag creates a flag record for a target.
func getOrCreateFlag(r *rand.Rand, ctx sdk.Context, k keeper.Keeper, targetType types.FlagTargetType, targetID uint64, flagger string) (string, error) {
	flagKey := keeper.FlagCompositeKey(targetType, targetID)
	_, err := k.Flag.Get(ctx, flagKey)
	if err == nil {
		return flagKey, nil
	}

	now := ctx.BlockHeight()
	weight := math.NewInt(int64(r.Intn(100) + 10))
	flag := types.CollectionFlag{
		TargetId:   targetID,
		TargetType: targetType,
		FlagRecords: []types.FlagRecord{
			{
				Flagger:    flagger,
				Reason:     randomModerationReason(r),
				ReasonText: "Simulation flag",
				FlaggedAt:  now,
				Weight:     weight,
			},
		},
		TotalWeight: weight,
		FirstFlagAt: now,
		LastFlagAt:  now,
	}

	if err := k.Flag.Set(ctx, flagKey, flag); err != nil {
		return "", err
	}
	return flagKey, nil
}

// getOrCreatePendingCollection creates or finds a PENDING collection for the owner.
func getOrCreatePendingCollection(r *rand.Rand, ctx sdk.Context, k keeper.Keeper, owner string) (uint64, error) {
	// Try to find existing PENDING collection by owner
	coll, collID, _ := findCollectionByOwner(r, ctx, k, owner)
	if coll != nil && coll.Status == types.CollectionStatus_COLLECTION_STATUS_PENDING {
		return collID, nil
	}

	collID, err := k.CollectionSeq.Next(ctx)
	if err != nil {
		return 0, err
	}

	now := ctx.BlockHeight()
	endorsementExpiry := now + int64(r.Intn(10000)+1000)

	newColl := types.Collection{
		Id:                       collID,
		Owner:                    owner,
		Name:                     randomCollectionName(r),
		Description:              randomDescription(r),
		Tags:                     []string{randomTag(r)},
		Type:                     randomCollectionType(r),
		Visibility:               types.Visibility_VISIBILITY_PUBLIC,
		Status:                   types.CollectionStatus_COLLECTION_STATUS_PENDING,
		CreatedAt:                now,
		UpdatedAt:                now,
		DepositAmount:            math.ZeroInt(),
		ItemDepositTotal:         math.ZeroInt(),
		CommunityFeedbackEnabled: true,
		Immutable:                false,
	}

	if err := k.Collection.Set(ctx, collID, newColl); err != nil {
		return 0, err
	}
	if err := k.CollectionsByOwner.Set(ctx, collections.Join(owner, collID)); err != nil {
		return 0, err
	}
	if err := k.CollectionsByStatus.Set(ctx, collections.Join(int32(newColl.Status), collID)); err != nil {
		return 0, err
	}
	if err := k.EndorsementPending.Set(ctx, collections.Join(endorsementExpiry, collID)); err != nil {
		return 0, err
	}

	return collID, nil
}

// --- Account helpers ---

// getAccountForAddress finds a sim account that matches the given address string.
func getAccountForAddress(addr string, accs []simtypes.Account) (simtypes.Account, bool) {
	for _, acc := range accs {
		if acc.Address.String() == addr {
			return acc, true
		}
	}
	return simtypes.Account{}, false
}

// pickDifferentAccount returns a random sim account that is NOT the given address.
func pickDifferentAccount(r *rand.Rand, accs []simtypes.Account, exclude string) (simtypes.Account, bool) {
	filtered := make([]simtypes.Account, 0, len(accs))
	for _, acc := range accs {
		if acc.Address.String() != exclude {
			filtered = append(filtered, acc)
		}
	}
	if len(filtered) == 0 {
		return simtypes.Account{}, false
	}
	return filtered[r.Intn(len(filtered))], true
}

// --- Random data generators ---

func randomCollectionName(r *rand.Rand) string {
	names := []string{
		"phoenix-gallery", "aurora-vault", "zenith-archive", "nebula-catalog",
		"prism-set", "vortex-shelf", "cascade-library", "ember-trove",
		"atlas-digest", "horizon-collection", "pulse-registry", "forge-index",
	}
	return names[r.Intn(len(names))]
}

func randomTitle(r *rand.Rand) string {
	titles := []string{
		"sunrise-entry", "moonlight-piece", "starfall-item", "dewdrop-record",
		"thunder-artifact", "coral-fragment", "crystal-shard", "amber-relic",
	}
	return titles[r.Intn(len(titles))]
}

func randomDescription(r *rand.Rand) string {
	descriptions := []string{
		"A simulation-generated entry for testing",
		"Sample content created during chain simulation",
		"Auto-generated description for sim coverage",
		"Test description from simulation framework",
	}
	return descriptions[r.Intn(len(descriptions))]
}

func randomTag(r *rand.Rand) string {
	tags := []string{
		"art", "science", "history", "tech", "nature",
		"music", "code", "design", "research", "education",
	}
	return tags[r.Intn(len(tags))]
}

func randomURI(r *rand.Rand) string {
	return fmt.Sprintf("ipfs://Qm%s", simtypes.RandStringOfLength(r, 40))
}

func randomCollectionType(r *rand.Rand) types.CollectionType {
	validTypes := []types.CollectionType{
		types.CollectionType_COLLECTION_TYPE_NFT,
		types.CollectionType_COLLECTION_TYPE_LINK,
		types.CollectionType_COLLECTION_TYPE_ONCHAIN,
		types.CollectionType_COLLECTION_TYPE_MIXED,
	}
	return validTypes[r.Intn(len(validTypes))]
}

func randomModerationReason(r *rand.Rand) types.ModerationReason {
	reasons := []types.ModerationReason{
		types.ModerationReason_MODERATION_REASON_SPAM,
		types.ModerationReason_MODERATION_REASON_HARASSMENT,
		types.ModerationReason_MODERATION_REASON_MISINFORMATION,
		types.ModerationReason_MODERATION_REASON_INAPPROPRIATE,
		types.ModerationReason_MODERATION_REASON_POLICY_VIOLATION,
	}
	return reasons[r.Intn(len(reasons))]
}

func randomCurationVerdict(r *rand.Rand) types.CurationVerdict {
	if r.Intn(2) == 0 {
		return types.CurationVerdict_CURATION_VERDICT_UP
	}
	return types.CurationVerdict_CURATION_VERDICT_DOWN
}

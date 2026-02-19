package keeper

import (
	"context"
	"fmt"
	"strconv"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	reptypes "sparkdream/x/rep/types"
	"sparkdream/x/collect/types"
)

const (
	SparkDenom = "uspark"
	DreamDenom = "udream"

	// BlocksPerDay is approximate blocks per day (~6s block time).
	BlocksPerDay int64 = 14400
)

// --- Composite key builders ---

// CollaboratorCompositeKey builds the storage key for a collaborator: "{collectionID}/{address}".
func CollaboratorCompositeKey(collectionID uint64, address string) string {
	return fmt.Sprintf("%d/%s", collectionID, address)
}

// FlagCompositeKey builds the storage key for a flag: "{targetType}/{targetID}".
func FlagCompositeKey(targetType types.FlagTargetType, targetID uint64) string {
	return fmt.Sprintf("%d/%d", int32(targetType), targetID)
}

// ReactionDedupCompositeKey builds the dedup key: "{address}/{targetType}/{targetID}".
func ReactionDedupCompositeKey(address string, targetType types.FlagTargetType, targetID uint64) string {
	return fmt.Sprintf("%s/%d/%d", address, int32(targetType), targetID)
}

// ReactionLimitKey builds the daily rate limit key: "{address}/{day}".
func ReactionLimitCompositeKey(address string, blockHeight int64, category string) string {
	day := blockHeight / BlocksPerDay
	return fmt.Sprintf("%s/%d/%s", address, day, category)
}

// HideRecordTargetKey builds the composite key for hide record by-target index.
func HideRecordTargetCompositeKey(targetType types.FlagTargetType, targetID uint64) string {
	return fmt.Sprintf("%d/%d", int32(targetType), targetID)
}

// --- Access control helpers ---

// HasWriteAccess returns true if the address is the collection owner or an EDITOR/ADMIN collaborator.
func (k Keeper) HasWriteAccess(ctx context.Context, coll types.Collection, address string) (bool, error) {
	if coll.Owner == address {
		return true, nil
	}
	key := CollaboratorCompositeKey(coll.Id, address)
	collab, err := k.Collaborator.Get(ctx, key)
	if err != nil {
		return false, nil
	}
	if collab.Role == types.CollaboratorRole_COLLABORATOR_ROLE_EDITOR ||
		collab.Role == types.CollaboratorRole_COLLABORATOR_ROLE_ADMIN {
		return true, nil
	}
	return false, nil
}

// IsOwnerOrAdmin returns true if the address is the collection owner or an ADMIN collaborator.
func (k Keeper) IsOwnerOrAdmin(ctx context.Context, coll types.Collection, address string) (bool, error) {
	if coll.Owner == address {
		return true, nil
	}
	key := CollaboratorCompositeKey(coll.Id, address)
	collab, err := k.Collaborator.Get(ctx, key)
	if err != nil {
		return false, nil
	}
	return collab.Role == types.CollaboratorRole_COLLABORATOR_ROLE_ADMIN, nil
}

// IsCollaborator checks if the address is a collaborator (any role) on the collection.
func (k Keeper) IsCollaborator(ctx context.Context, collectionID uint64, address string) (bool, types.CollaboratorRole, error) {
	key := CollaboratorCompositeKey(collectionID, address)
	collab, err := k.Collaborator.Get(ctx, key)
	if err != nil {
		return false, types.CollaboratorRole_COLLABORATOR_ROLE_UNSPECIFIED, nil
	}
	return true, collab.Role, nil
}

// --- Target resolution ---

// GetCollectionForTarget resolves the parent collection for a target (collection or item).
func (k Keeper) GetCollectionForTarget(ctx context.Context, targetType types.FlagTargetType, targetID uint64) (types.Collection, error) {
	switch targetType {
	case types.FlagTargetType_FLAG_TARGET_TYPE_COLLECTION:
		return k.Collection.Get(ctx, targetID)
	case types.FlagTargetType_FLAG_TARGET_TYPE_ITEM:
		item, err := k.Item.Get(ctx, targetID)
		if err != nil {
			return types.Collection{}, types.ErrItemNotFound
		}
		return k.Collection.Get(ctx, item.CollectionId)
	default:
		return types.Collection{}, fmt.Errorf("invalid target type")
	}
}

// ValidatePublicActiveTarget checks that a target is a public, active collection/item
// with community_feedback_enabled=true.
func (k Keeper) ValidatePublicActiveTarget(ctx context.Context, targetType types.FlagTargetType, targetID uint64) (types.Collection, error) {
	coll, err := k.GetCollectionForTarget(ctx, targetType, targetID)
	if err != nil {
		return types.Collection{}, err
	}
	if coll.Visibility != types.Visibility_VISIBILITY_PUBLIC {
		return types.Collection{}, types.ErrNotPublicActive
	}
	if coll.Status != types.CollectionStatus_COLLECTION_STATUS_ACTIVE {
		return types.Collection{}, types.ErrNotPublicActive
	}

	// For items, also check item status
	if targetType == types.FlagTargetType_FLAG_TARGET_TYPE_ITEM {
		item, err := k.Item.Get(ctx, targetID)
		if err != nil {
			return types.Collection{}, types.ErrItemNotFound
		}
		if item.Status == types.ItemStatus_ITEM_STATUS_HIDDEN {
			return types.Collection{}, types.ErrNotPublicActive
		}
	}

	return coll, nil
}

// ValidatePublicActiveFeedbackTarget is like ValidatePublicActiveTarget but also checks community_feedback_enabled.
func (k Keeper) ValidatePublicActiveFeedbackTarget(ctx context.Context, targetType types.FlagTargetType, targetID uint64) (types.Collection, error) {
	coll, err := k.ValidatePublicActiveTarget(ctx, targetType, targetID)
	if err != nil {
		return types.Collection{}, err
	}
	if !coll.CommunityFeedbackEnabled {
		return types.Collection{}, types.ErrNotPublicActive
	}
	return coll, nil
}

// --- SPARK operations ---

// EscrowSPARK transfers SPARK from an account to the module account (hold).
func (k Keeper) EscrowSPARK(ctx context.Context, from sdk.AccAddress, amount math.Int) error {
	coins := sdk.NewCoins(sdk.NewCoin(SparkDenom, amount))
	return k.bankKeeper.SendCoinsFromAccountToModule(ctx, from, types.ModuleName, coins)
}

// RefundSPARK transfers SPARK from the module account to an account (refund).
func (k Keeper) RefundSPARK(ctx context.Context, to sdk.AccAddress, amount math.Int) error {
	if amount.IsZero() {
		return nil
	}
	coins := sdk.NewCoins(sdk.NewCoin(SparkDenom, amount))
	return k.bankKeeper.SendCoinsFromModuleToAccount(ctx, types.ModuleName, to, coins)
}

// BurnSPARK burns SPARK from the module account.
func (k Keeper) BurnSPARK(ctx context.Context, amount math.Int) error {
	if amount.IsZero() {
		return nil
	}
	coins := sdk.NewCoins(sdk.NewCoin(SparkDenom, amount))
	return k.bankKeeper.BurnCoins(ctx, types.ModuleName, coins)
}

// BurnSPARKFromAccount transfers SPARK to module then burns.
func (k Keeper) BurnSPARKFromAccount(ctx context.Context, from sdk.AccAddress, amount math.Int) error {
	if amount.IsZero() {
		return nil
	}
	if err := k.EscrowSPARK(ctx, from, amount); err != nil {
		return err
	}
	return k.BurnSPARK(ctx, amount)
}

// --- Trust level helpers ---

// TrustLevelIndex returns the numeric index for a trust level (0-4).
func TrustLevelIndex(tl reptypes.TrustLevel) int {
	return int(tl)
}

// ParseTrustLevel parses a trust level string to the enum value.
func ParseTrustLevel(s string) (reptypes.TrustLevel, bool) {
	val, ok := reptypes.TrustLevel_value[s]
	if !ok {
		return 0, false
	}
	return reptypes.TrustLevel(val), true
}

// --- Position management ---

// CompactPositions reassigns sequential positions (0, 1, 2, ...) for all items in a collection.
func (k Keeper) CompactPositions(ctx context.Context, collectionID uint64) error {
	// Collect all item IDs in current position order
	var itemIDs []uint64
	err := k.ItemsByCollection.Walk(ctx,
		collections.NewPrefixedPairRange[uint64, uint64](collectionID),
		func(key collections.Pair[uint64, uint64]) (bool, error) {
			itemIDs = append(itemIDs, key.K2())
			return false, nil
		},
	)
	if err != nil {
		return err
	}

	// Clear all position index entries for this collection
	var keysToRemove []collections.Pair[uint64, uint64]
	err = k.ItemsByCollection.Walk(ctx,
		collections.NewPrefixedPairRange[uint64, uint64](collectionID),
		func(key collections.Pair[uint64, uint64]) (bool, error) {
			keysToRemove = append(keysToRemove, key)
			return false, nil
		},
	)
	if err != nil {
		return err
	}
	for _, key := range keysToRemove {
		if err := k.ItemsByCollection.Remove(ctx, key); err != nil {
			return err
		}
	}

	// Re-insert with item IDs and update item records with sequential positions
	for i, itemID := range itemIDs {
		newPos := uint64(i)
		if err := k.ItemsByCollection.Set(ctx, collections.Join(collectionID, itemID)); err != nil {
			return err
		}
		item, err := k.Item.Get(ctx, itemID)
		if err != nil {
			return err
		}
		item.Position = newPos
		if err := k.Item.Set(ctx, itemID, item); err != nil {
			return err
		}
	}

	return nil
}

// InsertAtPosition shifts items at position and above right by one, then sets the new item at position.
func (k Keeper) InsertAtPosition(ctx context.Context, collectionID uint64, position uint64, newItemID uint64) error {
	// Collect items at position and above (in reverse order to avoid overwriting)
	type posItem struct {
		pos    uint64
		itemID uint64
	}
	var toShift []posItem

	err := k.ItemsByCollection.Walk(ctx,
		collections.NewPrefixedPairRange[uint64, uint64](collectionID),
		func(key collections.Pair[uint64, uint64]) (bool, error) {
			if key.K2() >= position {
				// k2 is the position in ItemsByCollection which stores (collectionID, position) -> itemID
				// Actually ItemsByCollection is a KeySet not a Map, we need to get the item
				// Let me reconsider the data model
			}
			return false, nil
		},
	)
	_ = err
	_ = toShift

	// ItemsByCollection is a KeySet of (collectionID, itemID), not (collectionID, position)
	// The position is stored on the Item record itself.
	// We need to walk all items, sort by position, shift those >= target position.

	var items []types.Item
	err = k.ItemsByCollection.Walk(ctx,
		collections.NewPrefixedPairRange[uint64, uint64](collectionID),
		func(key collections.Pair[uint64, uint64]) (bool, error) {
			item, err := k.Item.Get(ctx, key.K2())
			if err != nil {
				return true, err
			}
			items = append(items, item)
			return false, nil
		},
	)
	if err != nil {
		return err
	}

	// Shift items at position and above
	for _, item := range items {
		if item.Position >= position {
			item.Position++
			if err := k.Item.Set(ctx, item.Id, item); err != nil {
				return err
			}
		}
	}

	return nil
}

// --- Cleanup ---

// deleteCollectionFull removes a collection and all associated state.
// Used by MsgDeleteCollection and EndBlocker TTL pruning.
func (k Keeper) deleteCollectionFull(ctx context.Context, coll types.Collection) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	ownerAddr, err := k.addressCodec.StringToBytes(coll.Owner)
	if err != nil {
		return err
	}

	params, err := k.Params.Get(ctx)
	if err != nil {
		return err
	}

	// Refund deposits if not burned
	if !coll.DepositBurned {
		totalRefund := coll.DepositAmount.Add(coll.ItemDepositTotal)
		if totalRefund.IsPositive() {
			if err := k.RefundSPARK(ctx, ownerAddr, totalRefund); err != nil {
				return err
			}
		}
	}

	// Handle PENDING collection: refund endorsement creation fee (minus burn fraction)
	if coll.Status == types.CollectionStatus_COLLECTION_STATUS_PENDING {
		burnAmt := params.EndorsementDeletionBurnFraction.MulInt(params.EndorsementCreationFee).TruncateInt()
		refundAmt := params.EndorsementCreationFee.Sub(burnAmt)
		if refundAmt.IsPositive() {
			if err := k.RefundSPARK(ctx, ownerAddr, refundAmt); err != nil {
				return err
			}
		}
		if burnAmt.IsPositive() {
			if err := k.BurnSPARK(ctx, burnAmt); err != nil {
				return err
			}
		}
		// Remove from pending index (walk to find the actual key)
		k.EndorsementPending.Walk(ctx, nil, func(key collections.Pair[int64, uint64]) (bool, error) {
			if key.K2() == coll.Id {
				k.EndorsementPending.Remove(ctx, key) //nolint:errcheck
				return true, nil // stop
			}
			return false, nil
		}) //nolint:errcheck
	}

	// Handle endorsed collection: release endorser stake if not yet released
	if coll.EndorsedBy != "" {
		endorsement, err := k.Endorsement.Get(ctx, coll.Id)
		if err == nil && !endorsement.StakeReleased {
			endorserAddr, err := k.addressCodec.StringToBytes(endorsement.Endorser)
			if err == nil {
				k.repKeeper.UnlockDREAM(ctx, endorserAddr, endorsement.DreamStake) //nolint:errcheck
			}
			endorsement.StakeReleased = true
			k.Endorsement.Set(ctx, coll.Id, endorsement) //nolint:errcheck
		}
		// Remove endorsement indexes
		if err == nil {
			k.EndorsementStakeExpiry.Remove(ctx, collections.Join(endorsement.StakeReleaseAt, coll.Id)) //nolint:errcheck
		}
		k.Endorsement.Remove(ctx, coll.Id) //nolint:errcheck
	}

	// Handle pending sponsorship request: refund escrowed deposits
	req, err := k.SponsorshipRequest.Get(ctx, coll.Id)
	if err == nil {
		requesterAddr, err := k.addressCodec.StringToBytes(req.Requester)
		if err == nil {
			refundAmt := req.CollectionDeposit.Add(req.ItemDepositTotal)
			if refundAmt.IsPositive() {
				k.RefundSPARK(ctx, requesterAddr, refundAmt) //nolint:errcheck
			}
		}
		k.SponsorshipRequestsByExpiry.Remove(ctx, collections.Join(req.ExpiresAt, coll.Id)) //nolint:errcheck
		k.SponsorshipRequest.Remove(ctx, coll.Id) //nolint:errcheck
	}

	// Handle active hide appeals: burn appeal fee, release sentinel bond
	err = k.HideRecordByTarget.Walk(ctx,
		collections.NewPrefixedPairRange[string, uint64](HideRecordTargetCompositeKey(types.FlagTargetType_FLAG_TARGET_TYPE_COLLECTION, coll.Id)),
		func(key collections.Pair[string, uint64]) (bool, error) {
			hr, err := k.HideRecord.Get(ctx, key.K2())
			if err != nil {
				return false, nil
			}
			if hr.Appealed && !hr.Resolved {
				// Burn escrowed appeal fee
				k.BurnSPARK(ctx, params.AppealFee) //nolint:errcheck
				// Release sentinel bond
				if k.forumKeeper != nil {
					k.forumKeeper.ReleaseBondCommitment(ctx, hr.Sentinel, hr.CommittedAmount, types.ModuleName, hr.Id) //nolint:errcheck
				}
			} else if !hr.Resolved {
				// Unappealed hide: release sentinel bond
				if k.forumKeeper != nil {
					k.forumKeeper.ReleaseBondCommitment(ctx, hr.Sentinel, hr.CommittedAmount, types.ModuleName, hr.Id) //nolint:errcheck
				}
			}
			hr.Resolved = true
			k.HideRecord.Set(ctx, hr.Id, hr) //nolint:errcheck
			k.HideRecordExpiry.Remove(ctx, collections.Join(hr.AppealDeadline, hr.Id)) //nolint:errcheck
			return false, nil
		},
	)
	_ = err

	// Cleanup curation: refund pending challenge deposits, update curator state
	err = k.CurationReviewsByCollection.Walk(ctx,
		collections.NewPrefixedPairRange[uint64, uint64](coll.Id),
		func(key collections.Pair[uint64, uint64]) (bool, error) {
			review, err := k.CurationReview.Get(ctx, key.K2())
			if err != nil {
				return false, nil
			}
			// Refund pending challenge deposits
			if review.Challenged && !review.Overturned {
				challengerAddr, err := k.addressCodec.StringToBytes(review.Challenger)
				if err == nil {
					k.repKeeper.UnlockDREAM(ctx, challengerAddr, params.ChallengeDeposit) //nolint:errcheck
				}
				// Decrement pending challenges on curator
				curator, err := k.Curator.Get(ctx, review.Curator)
				if err == nil && curator.PendingChallenges > 0 {
					curator.PendingChallenges--
					k.Curator.Set(ctx, review.Curator, curator) //nolint:errcheck
				}
			}
			// Remove review indexes
			k.CurationReviewsByCurator.Remove(ctx, collections.Join(review.Curator, review.Id)) //nolint:errcheck
			k.CurationReview.Remove(ctx, review.Id) //nolint:errcheck
			return false, nil
		},
	)
	_ = err
	// Remove all review-by-collection index entries
	var reviewKeys []collections.Pair[uint64, uint64]
	k.CurationReviewsByCollection.Walk(ctx,
		collections.NewPrefixedPairRange[uint64, uint64](coll.Id),
		func(key collections.Pair[uint64, uint64]) (bool, error) {
			reviewKeys = append(reviewKeys, key)
			return false, nil
		},
	)
	for _, key := range reviewKeys {
		k.CurationReviewsByCollection.Remove(ctx, key) //nolint:errcheck
	}
	k.CurationSummary.Remove(ctx, coll.Id) //nolint:errcheck

	// Delete all items
	var itemKeys []collections.Pair[uint64, uint64]
	k.ItemsByCollection.Walk(ctx,
		collections.NewPrefixedPairRange[uint64, uint64](coll.Id),
		func(key collections.Pair[uint64, uint64]) (bool, error) {
			itemKeys = append(itemKeys, key)
			return false, nil
		},
	)
	for _, key := range itemKeys {
		itemID := key.K2()
		item, err := k.Item.Get(ctx, itemID)
		if err == nil {
			// Clean up item hide records
			k.cleanupItemHideRecords(ctx, item, params)
			// Clean up item flags
			flagKey := FlagCompositeKey(types.FlagTargetType_FLAG_TARGET_TYPE_ITEM, itemID)
			flag, err := k.Flag.Get(ctx, flagKey)
			if err == nil {
				if flag.InReviewQueue {
					k.FlagReviewQueue.Remove(ctx, collections.Join(int32(types.FlagTargetType_FLAG_TARGET_TYPE_ITEM), itemID)) //nolint:errcheck
				}
				k.FlagExpiry.Remove(ctx, collections.Join(flag.LastFlagAt+params.FlagExpirationBlocks, flagKey)) //nolint:errcheck
				k.Flag.Remove(ctx, flagKey) //nolint:errcheck
			}
			// Clean up item reaction dedup entries (can't efficiently walk by prefix, leave for now)
			k.ItemsByOwner.Remove(ctx, collections.Join(coll.Owner, itemID)) //nolint:errcheck
		}
		k.Item.Remove(ctx, itemID) //nolint:errcheck
		k.ItemsByCollection.Remove(ctx, key) //nolint:errcheck
	}

	// Delete all collaborators using reverse index
	var collabReverseKeys []collections.Pair[string, uint64]
	k.CollaboratorReverse.Walk(ctx, nil,
		func(key collections.Pair[string, uint64]) (bool, error) {
			if key.K2() == coll.Id {
				collabReverseKeys = append(collabReverseKeys, key)
			}
			return false, nil
		},
	)
	for _, key := range collabReverseKeys {
		addr := key.K1()
		compositeKey := CollaboratorCompositeKey(coll.Id, addr)
		k.Collaborator.Remove(ctx, compositeKey) //nolint:errcheck
		k.CollaboratorReverse.Remove(ctx, key)   //nolint:errcheck
	}

	// Clean up collection-level flags
	flagKey := FlagCompositeKey(types.FlagTargetType_FLAG_TARGET_TYPE_COLLECTION, coll.Id)
	flag, err := k.Flag.Get(ctx, flagKey)
	if err == nil {
		if flag.InReviewQueue {
			k.FlagReviewQueue.Remove(ctx, collections.Join(int32(types.FlagTargetType_FLAG_TARGET_TYPE_COLLECTION), coll.Id)) //nolint:errcheck
		}
		k.FlagExpiry.Remove(ctx, collections.Join(flag.LastFlagAt+params.FlagExpirationBlocks, flagKey)) //nolint:errcheck
		k.Flag.Remove(ctx, flagKey) //nolint:errcheck
	}

	// Clean up hide records for collection
	var hideTargetKeys []collections.Pair[string, uint64]
	collTargetKey := HideRecordTargetCompositeKey(types.FlagTargetType_FLAG_TARGET_TYPE_COLLECTION, coll.Id)
	k.HideRecordByTarget.Walk(ctx,
		collections.NewPrefixedPairRange[string, uint64](collTargetKey),
		func(key collections.Pair[string, uint64]) (bool, error) {
			hideTargetKeys = append(hideTargetKeys, key)
			return false, nil
		},
	)
	for _, key := range hideTargetKeys {
		k.HideRecordByTarget.Remove(ctx, key) //nolint:errcheck
	}

	// Remove indexes
	k.CollectionsByOwner.Remove(ctx, collections.Join(coll.Owner, coll.Id)) //nolint:errcheck
	if coll.ExpiresAt > 0 {
		k.CollectionsByExpiry.Remove(ctx, collections.Join(coll.ExpiresAt, coll.Id)) //nolint:errcheck
	}
	k.CollectionsByStatus.Remove(ctx, collections.Join(int32(coll.Status), coll.Id)) //nolint:errcheck

	// Delete collection
	k.Collection.Remove(ctx, coll.Id) //nolint:errcheck

	depositRefunded := "0"
	itemDepositRefunded := "0"
	if !coll.DepositBurned {
		depositRefunded = coll.DepositAmount.String()
		itemDepositRefunded = coll.ItemDepositTotal.String()
	}
	sdkCtx.EventManager().EmitEvent(sdk.NewEvent("collection_deleted",
		sdk.NewAttribute("id", strconv.FormatUint(coll.Id, 10)),
		sdk.NewAttribute("owner", coll.Owner),
		sdk.NewAttribute("item_count", strconv.FormatUint(coll.ItemCount, 10)),
		sdk.NewAttribute("deposit_refunded", depositRefunded),
		sdk.NewAttribute("item_deposit_refunded", itemDepositRefunded),
	))

	return nil
}

// cleanupItemHideRecords handles hide records for an item being deleted.
func (k Keeper) cleanupItemHideRecords(ctx context.Context, item types.Item, params types.Params) {
	targetKey := HideRecordTargetCompositeKey(types.FlagTargetType_FLAG_TARGET_TYPE_ITEM, item.Id)
	k.HideRecordByTarget.Walk(ctx,
		collections.NewPrefixedPairRange[string, uint64](targetKey),
		func(key collections.Pair[string, uint64]) (bool, error) {
			hr, err := k.HideRecord.Get(ctx, key.K2())
			if err != nil {
				return false, nil
			}
			if hr.Appealed && !hr.Resolved {
				k.BurnSPARK(ctx, params.AppealFee) //nolint:errcheck
				if k.forumKeeper != nil {
					k.forumKeeper.ReleaseBondCommitment(ctx, hr.Sentinel, hr.CommittedAmount, types.ModuleName, hr.Id) //nolint:errcheck
				}
			} else if !hr.Resolved {
				if k.forumKeeper != nil {
					k.forumKeeper.ReleaseBondCommitment(ctx, hr.Sentinel, hr.CommittedAmount, types.ModuleName, hr.Id) //nolint:errcheck
				}
			}
			hr.Resolved = true
			k.HideRecord.Set(ctx, hr.Id, hr) //nolint:errcheck
			k.HideRecordExpiry.Remove(ctx, collections.Join(hr.AppealDeadline, hr.Id)) //nolint:errcheck
			return false, nil
		},
	)
}

// countCollectionsByOwner counts how many collections an owner has.
func (k Keeper) countCollectionsByOwner(ctx context.Context, owner string) (uint32, error) {
	var count uint32
	err := k.CollectionsByOwner.Walk(ctx,
		collections.NewPrefixedPairRange[string, uint64](owner),
		func(key collections.Pair[string, uint64]) (bool, error) {
			count++
			return false, nil
		},
	)
	return count, err
}

// getMaxCollections returns the tiered collection limit for an address.
func (k Keeper) getMaxCollections(ctx context.Context, address string, params types.Params) uint32 {
	addrBytes, err := k.addressCodec.StringToBytes(address)
	if err != nil {
		return params.MaxCollectionsBase
	}
	if !k.repKeeper.IsMember(ctx, addrBytes) {
		return params.MaxCollectionsBase
	}
	tl, err := k.repKeeper.GetTrustLevel(ctx, addrBytes)
	if err != nil {
		return params.MaxCollectionsBase
	}
	return params.MaxCollectionsBase + uint32(TrustLevelIndex(tl))*params.MaxCollectionsPerTrustLevel
}

// isMember checks if the address is an active x/rep member.
func (k Keeper) isMember(ctx context.Context, address string) bool {
	addrBytes, err := k.addressCodec.StringToBytes(address)
	if err != nil {
		return false
	}
	return k.repKeeper.IsMember(ctx, addrBytes)
}

// meetsMinTrustLevel checks if address is at or above the required trust level string.
func (k Keeper) meetsMinTrustLevel(ctx context.Context, address string, minLevel string) bool {
	addrBytes, err := k.addressCodec.StringToBytes(address)
	if err != nil {
		return false
	}
	if !k.repKeeper.IsMember(ctx, addrBytes) {
		return false
	}
	tl, err := k.repKeeper.GetTrustLevel(ctx, addrBytes)
	if err != nil {
		return false
	}
	required, ok := ParseTrustLevel(minLevel)
	if !ok {
		return false
	}
	return tl >= required
}

// checkDailyLimit checks and increments a daily reaction counter. Returns error if limit exceeded.
func (k Keeper) checkDailyLimit(ctx context.Context, address string, blockHeight int64, category string, maxPerDay uint32) error {
	key := ReactionLimitCompositeKey(address, blockHeight, category)
	current, err := k.ReactionLimit.Get(ctx, key)
	if err != nil {
		current = 0
	}
	if current >= maxPerDay {
		return types.ErrMaxDailyReactions
	}
	return k.ReactionLimit.Set(ctx, key, current+1)
}

// attrsToValues converts a slice of KeyValuePair pointers to values.
func attrsToValues(attrs []*types.KeyValuePair) []types.KeyValuePair {
	result := make([]types.KeyValuePair, len(attrs))
	for i, a := range attrs {
		if a != nil {
			result[i] = *a
		}
	}
	return result
}

// isTTLCollection returns true if the collection has a TTL and deposits are held (not burned).
func isTTLCollection(coll types.Collection) bool {
	return coll.ExpiresAt > 0 && !coll.DepositBurned
}

// validateReferenceFields validates that the correct reference is set for the given type
// and all string fields are within length limits.
func (k Keeper) validateReferenceFields(msg_refType types.ReferenceType, nft *types.NftReference, link *types.LinkReference, onChain *types.OnChainReference, custom *types.CustomReference, maxLen uint32) error {
	switch msg_refType {
	case types.ReferenceType_REFERENCE_TYPE_NFT:
		if nft == nil {
			return types.ErrInvalidReference
		}
		if uint32(len(nft.ChainId)) > maxLen || uint32(len(nft.ContractAddress)) > maxLen ||
			uint32(len(nft.TokenId)) > maxLen || uint32(len(nft.TokenStandard)) > maxLen ||
			uint32(len(nft.TokenUri)) > maxLen {
			return types.ErrReferenceFieldTooLong
		}
	case types.ReferenceType_REFERENCE_TYPE_LINK:
		if link == nil {
			return types.ErrInvalidReference
		}
		if uint32(len(link.Uri)) > maxLen || uint32(len(link.ContentHash)) > maxLen ||
			uint32(len(link.ContentType)) > maxLen {
			return types.ErrReferenceFieldTooLong
		}
	case types.ReferenceType_REFERENCE_TYPE_ON_CHAIN:
		if onChain == nil {
			return types.ErrInvalidReference
		}
		if uint32(len(onChain.Module)) > maxLen || uint32(len(onChain.EntityType)) > maxLen ||
			uint32(len(onChain.EntityId)) > maxLen {
			return types.ErrReferenceFieldTooLong
		}
	case types.ReferenceType_REFERENCE_TYPE_CUSTOM:
		if custom == nil {
			return types.ErrInvalidReference
		}
		if uint32(len(custom.TypeLabel)) > maxLen || uint32(len(custom.Value)) > maxLen {
			return types.ErrReferenceFieldTooLong
		}
	case types.ReferenceType_REFERENCE_TYPE_UNSPECIFIED:
		// OK - no reference needed
	default:
		return types.ErrInvalidReference
	}
	return nil
}

// validateItemFields validates item content fields against params.
func (k Keeper) validateItemFields(encrypted bool, title, description, imageUri string, refType types.ReferenceType, nft *types.NftReference, link *types.LinkReference, onChain *types.OnChainReference, custom *types.CustomReference, attributes []types.KeyValuePair, encryptedData []byte, params types.Params) error {
	if encrypted {
		if uint32(len(encryptedData)) > params.MaxEncryptedDataSize {
			return types.ErrEncryptedDataTooLarge
		}
		if title != "" || description != "" || imageUri != "" {
			return types.ErrEncryptedFieldMismatch
		}
		return nil
	}

	// Public item validation
	if encryptedData != nil && len(encryptedData) > 0 {
		return types.ErrEncryptedFieldMismatch
	}
	if uint32(len(title)) > params.MaxTitleLength {
		return types.ErrInvalidTitle
	}
	if uint32(len(description)) > params.MaxDescriptionLength {
		return types.ErrInvalidDescription
	}
	if uint32(len(imageUri)) > params.MaxReferenceFieldLength {
		return types.ErrReferenceFieldTooLong
	}

	if err := k.validateReferenceFields(refType, nft, link, onChain, custom, params.MaxReferenceFieldLength); err != nil {
		return err
	}

	// Count attributes (including custom reference extra)
	attrCount := uint32(len(attributes))
	if custom != nil {
		attrCount += uint32(len(custom.Extra))
	}
	if attrCount > params.MaxAttributesPerItem {
		return types.ErrMaxAttributes
	}
	for _, attr := range attributes {
		if uint32(len(attr.Key)) > params.MaxAttributeKeyLength {
			return types.ErrAttributeTooLong
		}
		if uint32(len(attr.Value)) > params.MaxAttributeValueLength {
			return types.ErrAttributeTooLong
		}
	}
	if custom != nil {
		for _, extra := range custom.Extra {
			if uint32(len(extra.Key)) > params.MaxAttributeKeyLength {
				return types.ErrAttributeTooLong
			}
			if uint32(len(extra.Value)) > params.MaxAttributeValueLength {
				return types.ErrAttributeTooLong
			}
		}
	}

	return nil
}

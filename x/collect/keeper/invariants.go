package keeper

import (
	"fmt"

	"cosmossdk.io/collections"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"sparkdream/x/collect/types"
)

// RegisterInvariants registers all x/collect module invariants.
func RegisterInvariants(ir sdk.InvariantRegistry, k Keeper) {
	ir.RegisterRoute(types.ModuleName, "collection-counter", CollectionCounterInvariant(k))
	ir.RegisterRoute(types.ModuleName, "item-counter", ItemCounterInvariant(k))
	ir.RegisterRoute(types.ModuleName, "item-collection-reference", ItemCollectionReferenceInvariant(k))
	ir.RegisterRoute(types.ModuleName, "hide-record-consistency", HideRecordConsistencyInvariant(k))
	ir.RegisterRoute(types.ModuleName, "status-index-consistency", StatusIndexConsistencyInvariant(k))
}

// CollectionCounterInvariant checks that CollectionSeq is greater than every
// stored collection ID.
func CollectionCounterInvariant(k Keeper) sdk.Invariant {
	return func(ctx sdk.Context) (string, bool) {
		collSeq, err := k.CollectionSeq.Peek(ctx)
		if err != nil {
			return sdk.FormatInvariant(types.ModuleName, "collection-counter",
				fmt.Sprintf("failed to read collection sequence: %v", err)), true
		}

		var broken int
		var msg string

		err = k.Collection.Walk(ctx, nil, func(id uint64, coll types.Collection) (bool, error) {
			if coll.Id >= collSeq {
				broken++
				msg += fmt.Sprintf("  collection ID %d >= CollectionSeq %d\n", coll.Id, collSeq)
			}
			return false, nil
		})
		if err != nil {
			return sdk.FormatInvariant(types.ModuleName, "collection-counter",
				fmt.Sprintf("error walking collections: %v", err)), true
		}

		return sdk.FormatInvariant(types.ModuleName, "collection-counter",
			fmt.Sprintf("found %d collection counter violations\n%s", broken, msg)), broken > 0
	}
}

// ItemCounterInvariant checks that ItemSeq is greater than every stored item ID.
func ItemCounterInvariant(k Keeper) sdk.Invariant {
	return func(ctx sdk.Context) (string, bool) {
		itemSeq, err := k.ItemSeq.Peek(ctx)
		if err != nil {
			return sdk.FormatInvariant(types.ModuleName, "item-counter",
				fmt.Sprintf("failed to read item sequence: %v", err)), true
		}

		var broken int
		var msg string

		err = k.Item.Walk(ctx, nil, func(id uint64, item types.Item) (bool, error) {
			if item.Id >= itemSeq {
				broken++
				msg += fmt.Sprintf("  item ID %d >= ItemSeq %d\n", item.Id, itemSeq)
			}
			return false, nil
		})
		if err != nil {
			return sdk.FormatInvariant(types.ModuleName, "item-counter",
				fmt.Sprintf("error walking items: %v", err)), true
		}

		return sdk.FormatInvariant(types.ModuleName, "item-counter",
			fmt.Sprintf("found %d item counter violations\n%s", broken, msg)), broken > 0
	}
}

// ItemCollectionReferenceInvariant checks that every item references an
// existing collection, and that the ItemsByCollection index is consistent.
func ItemCollectionReferenceInvariant(k Keeper) sdk.Invariant {
	return func(ctx sdk.Context) (string, bool) {
		var broken int
		var msg string

		err := k.Item.Walk(ctx, nil, func(id uint64, item types.Item) (bool, error) {
			_, err := k.Collection.Get(ctx, item.CollectionId)
			if err != nil {
				broken++
				msg += fmt.Sprintf("  item %d references non-existent collection %d\n",
					id, item.CollectionId)
				return false, nil
			}

			// Verify index entry exists
			has, err := k.ItemsByCollection.Has(ctx, collections.Join(item.CollectionId, item.Id))
			if err != nil || !has {
				broken++
				msg += fmt.Sprintf("  item %d missing from ItemsByCollection index for collection %d\n",
					id, item.CollectionId)
			}
			return false, nil
		})
		if err != nil {
			return sdk.FormatInvariant(types.ModuleName, "item-collection-reference",
				fmt.Sprintf("error walking items: %v", err)), true
		}

		return sdk.FormatInvariant(types.ModuleName, "item-collection-reference",
			fmt.Sprintf("found %d item-collection reference violations\n%s", broken, msg)), broken > 0
	}
}

// HideRecordConsistencyInvariant checks that every unresolved HideRecord
// references a target that is in HIDDEN status.
func HideRecordConsistencyInvariant(k Keeper) sdk.Invariant {
	return func(ctx sdk.Context) (string, bool) {
		var broken int
		var msg string

		err := k.HideRecord.Walk(ctx, nil, func(id uint64, hr types.HideRecord) (bool, error) {
			if hr.Resolved {
				return false, nil // skip resolved records
			}

			switch hr.TargetType {
			case types.FlagTargetType_FLAG_TARGET_TYPE_COLLECTION:
				coll, err := k.Collection.Get(ctx, hr.TargetId)
				if err != nil {
					broken++
					msg += fmt.Sprintf("  hide record %d references non-existent collection %d\n",
						id, hr.TargetId)
				} else if coll.Status != types.CollectionStatus_COLLECTION_STATUS_HIDDEN {
					broken++
					msg += fmt.Sprintf("  hide record %d: collection %d has status %s, expected HIDDEN\n",
						id, hr.TargetId, coll.Status.String())
				}
			case types.FlagTargetType_FLAG_TARGET_TYPE_ITEM:
				item, err := k.Item.Get(ctx, hr.TargetId)
				if err != nil {
					broken++
					msg += fmt.Sprintf("  hide record %d references non-existent item %d\n",
						id, hr.TargetId)
				} else if item.Status != types.ItemStatus_ITEM_STATUS_HIDDEN {
					broken++
					msg += fmt.Sprintf("  hide record %d: item %d has status %s, expected HIDDEN\n",
						id, hr.TargetId, item.Status.String())
				}
			}
			return false, nil
		})
		if err != nil {
			return sdk.FormatInvariant(types.ModuleName, "hide-record-consistency",
				fmt.Sprintf("error walking hide records: %v", err)), true
		}

		return sdk.FormatInvariant(types.ModuleName, "hide-record-consistency",
			fmt.Sprintf("found %d hide record consistency violations\n%s", broken, msg)), broken > 0
	}
}

// StatusIndexConsistencyInvariant checks that the CollectionsByStatus index
// is consistent with actual collection statuses.
func StatusIndexConsistencyInvariant(k Keeper) sdk.Invariant {
	return func(ctx sdk.Context) (string, bool) {
		var broken int
		var msg string

		// Walk all collections and verify their status index entry
		err := k.Collection.Walk(ctx, nil, func(id uint64, coll types.Collection) (bool, error) {
			has, err := k.CollectionsByStatus.Has(ctx, collections.Join(int32(coll.Status), coll.Id))
			if err != nil || !has {
				broken++
				msg += fmt.Sprintf("  collection %d (status %s) missing from CollectionsByStatus index\n",
					id, coll.Status.String())
			}
			return false, nil
		})
		if err != nil {
			return sdk.FormatInvariant(types.ModuleName, "status-index-consistency",
				fmt.Sprintf("error walking collections: %v", err)), true
		}

		return sdk.FormatInvariant(types.ModuleName, "status-index-consistency",
			fmt.Sprintf("found %d status index consistency violations\n%s", broken, msg)), broken > 0
	}
}

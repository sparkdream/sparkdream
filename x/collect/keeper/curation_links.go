package keeper

import (
	"context"
	"errors"

	"cosmossdk.io/collections"

	"sparkdream/x/collect/types"
)

// ActiveLinkURIs returns the URIs of a collection's active link items, and
// whether the collection exists and is active. x/federation reads it for a
// peer whose author gate names this collection (PeerPolicy.curation): the
// collection's owner and collaborators curate the list, and a hidden item or
// a collection that is not active admits nobody.
func (k Keeper) ActiveLinkURIs(ctx context.Context, collectionID uint64) ([]string, bool, error) {
	coll, err := k.Collection.Get(ctx, collectionID)
	if errors.Is(err, collections.ErrNotFound) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if coll.Status != types.CollectionStatus_COLLECTION_STATUS_ACTIVE {
		return nil, false, nil
	}
	var uris []string
	err = k.ItemsByCollection.Walk(ctx,
		collections.NewPrefixedPairRange[uint64, uint64](collectionID),
		func(key collections.Pair[uint64, uint64]) (bool, error) {
			item, err := k.Item.Get(ctx, key.K2())
			if err != nil {
				if errors.Is(err, collections.ErrNotFound) {
					return false, nil
				}
				return true, err
			}
			if item.Status == types.ItemStatus_ITEM_STATUS_ACTIVE &&
				item.ReferenceType == types.ReferenceType_REFERENCE_TYPE_LINK &&
				item.Link != nil && item.Link.Uri != "" {
				uris = append(uris, item.Link.Uri)
			}
			return false, nil
		},
	)
	if err != nil {
		return nil, false, err
	}
	return uris, true, nil
}

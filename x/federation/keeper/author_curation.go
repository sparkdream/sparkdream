package keeper

import (
	"context"

	errorsmod "cosmossdk.io/errors"

	"sparkdream/x/federation/types"
)

// CheckAuthorAdmitted applies a peer policy's two author gates to a bridged
// submission (see PeerPolicy.allowed_identities / curation):
//
//	allowed_identities  "*" admits anyone; otherwise the author must be
//	                    listed. Empty admits nobody.
//	curation            when set, the author must also be an active link
//	                    item of the named, active x/collect collection.
//
// An unattributed submission (empty creator_identity) can only pass an open
// policy: "*" and no curation collection.
func (k Keeper) CheckAuthorAdmitted(ctx context.Context, policy types.PeerPolicy, creatorIdentity string) error {
	author, attributed := types.NormalizeAuthorIdentity(creatorIdentity)

	open := false
	listed := false
	for _, e := range policy.AllowedIdentities {
		if e == types.AllIdentities {
			open = true
			break
		}
		if n, ok := types.NormalizeAuthorIdentity(e); ok && attributed && n == author {
			listed = true
		}
	}
	if !open && !listed {
		return errorsmod.Wrapf(types.ErrIdentityNotAllowed, "author %q is not admitted for peer %s", creatorIdentity, policy.PeerId)
	}

	if policy.Curation == nil {
		return nil
	}
	uris, ok, err := k.curationLinks(ctx, policy.Curation.CollectionId)
	if err != nil {
		return err
	}
	if !ok {
		return errorsmod.Wrapf(types.ErrCurationUnavailable, "collection %d (peer %s)", policy.Curation.CollectionId, policy.PeerId)
	}
	if attributed {
		for _, uri := range uris {
			if n, ok := types.NormalizeAuthorIdentity(uri); ok && n == author {
				return nil
			}
		}
	}
	return errorsmod.Wrapf(types.ErrIdentityNotCurated, "author %q is not in collection %d (peer %s)",
		creatorIdentity, policy.Curation.CollectionId, policy.PeerId)
}

// curationLinks reads a curation collection through the late-wired x/collect
// keeper; false when there is no such active collection (or no x/collect).
func (k Keeper) curationLinks(ctx context.Context, collectionID uint64) ([]string, bool, error) {
	if k.late == nil || k.late.collectKeeper == nil {
		return nil, false, nil
	}
	return k.late.collectKeeper.ActiveLinkURIs(ctx, collectionID)
}

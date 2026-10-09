package keeper

import (
	errorsmod "cosmossdk.io/errors"

	"sparkdream/x/collect/types"
	commontypes "sparkdream/x/common/types"
)

// Collect records never carry media bytes: a data URI in any of the string
// fields below is rejected at write, so labels come from which URI fields are
// set, never from a scan. Collect records are not withheld from queries.
// See docs/content-scanning.md §3.3.

// itemDataURIFields lists the item fields that must not hold a data URI.
func itemDataURIFields(imageUri string, nft *types.NftReference, link *types.LinkReference, custom *types.CustomReference, attributes []types.KeyValuePair) []string {
	fields := []string{imageUri}
	if nft != nil {
		fields = append(fields, nft.TokenUri)
	}
	if link != nil {
		fields = append(fields, link.Uri)
	}
	if custom != nil {
		fields = append(fields, custom.Value)
		for _, extra := range custom.Extra {
			fields = append(fields, extra.Value)
		}
	}
	for _, attr := range attributes {
		fields = append(fields, attr.Value)
	}
	return fields
}

// rejectDataURIs fails if any field contains a data URI.
func rejectDataURIs(fields ...string) error {
	for _, f := range fields {
		if commontypes.ContainsDataURI(f) {
			return errorsmod.Wrap(types.ErrDataURINotAllowed, "collect fields reference content elsewhere; embed nothing inline")
		}
	}
	return nil
}

// applyItemMediaLabels recomputes an item's chain-owned media labels:
// EXTERNAL_URI when image_uri is set or the reference is a LINK or NFT.
// INLINE_DATA only arises for records imported from a genesis that predates
// the data-URI rejection; clients do not render the affected fields.
func applyItemMediaLabels(item *types.Item) {
	var flags uint32
	if item.ImageUri != "" ||
		item.ReferenceType == types.ReferenceType_REFERENCE_TYPE_LINK ||
		item.ReferenceType == types.ReferenceType_REFERENCE_TYPE_NFT {
		flags |= uint32(commontypes.MediaFlag_MEDIA_FLAG_EXTERNAL_URI)
	}
	if rejectDataURIs(itemDataURIFields(item.ImageUri, item.Nft, item.Link, item.Custom, attrsToValues(item.Attributes))...) != nil {
		flags |= uint32(commontypes.MediaFlag_MEDIA_FLAG_INLINE_DATA)
	}
	item.MediaFlags, item.MediaRulesVersion = flags, commontypes.MediaRulesVersion
}

// applyCollectionMediaLabels recomputes a collection's chain-owned media
// labels: EXTERNAL_URI when cover_uri is set (INLINE_DATA as for items).
func applyCollectionMediaLabels(coll *types.Collection) {
	var flags uint32
	if coll.CoverUri != "" {
		flags |= uint32(commontypes.MediaFlag_MEDIA_FLAG_EXTERNAL_URI)
	}
	if commontypes.ContainsDataURI(coll.CoverUri) {
		flags |= uint32(commontypes.MediaFlag_MEDIA_FLAG_INLINE_DATA)
	}
	coll.MediaFlags, coll.MediaRulesVersion = flags, commontypes.MediaRulesVersion
}

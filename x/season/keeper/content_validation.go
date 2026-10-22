package keeper

import (
	"context"
	"strconv"
	"strings"

	"sparkdream/x/season/types"
)

// ValidateContentRef validates a content reference string.
// Supported formats: blog/post/{id}, forum/post/{id}, collect/collection/{id}, rep/initiative/{id}, rep/jury/{address},
// artifact/certificate/{class_id}/{edition_id}
func (k Keeper) ValidateContentRef(ctx context.Context, contentRef string) error {
	parts := strings.Split(contentRef, "/")
	if len(parts) < 3 {
		return types.ErrInvalidContentRef.Wrapf("expected format: module/type/id, got: %s", contentRef)
	}

	module := parts[0]
	contentType := parts[1]
	identifier := strings.Join(parts[2:], "/") // for addresses that might contain "/"

	switch module {
	case "blog":
		if contentType != "post" {
			return types.ErrInvalidContentRef.Wrapf("unsupported blog content type: %s", contentType)
		}
		id, err := strconv.ParseUint(identifier, 10, 64)
		if err != nil {
			return types.ErrInvalidContentRef.Wrapf("invalid blog post ID: %s", identifier)
		}
		if k.blogKeeper != nil {
			if !k.blogKeeper.HasPost(ctx, id) {
				return types.ErrContentNotFound.Wrapf("blog post %d not found", id)
			}
		}
	case "forum":
		if contentType != "post" {
			return types.ErrInvalidContentRef.Wrapf("unsupported forum content type: %s", contentType)
		}
		id, err := strconv.ParseUint(identifier, 10, 64)
		if err != nil {
			return types.ErrInvalidContentRef.Wrapf("invalid forum post ID: %s", identifier)
		}
		if k.forumKeeper != nil {
			if !k.forumKeeper.HasPost(ctx, id) {
				return types.ErrContentNotFound.Wrapf("forum post %d not found", id)
			}
		}
	case "collect":
		if contentType != "collection" {
			return types.ErrInvalidContentRef.Wrapf("unsupported collect content type: %s", contentType)
		}
		id, err := strconv.ParseUint(identifier, 10, 64)
		if err != nil {
			return types.ErrInvalidContentRef.Wrapf("invalid collection ID: %s", identifier)
		}
		if k.collectKeeper != nil {
			if !k.collectKeeper.HasCollection(ctx, id) {
				return types.ErrContentNotFound.Wrapf("collection %d not found", id)
			}
		}
	case "rep":
		if contentType == "initiative" {
			_, err := strconv.ParseUint(identifier, 10, 64)
			if err != nil {
				return types.ErrInvalidContentRef.Wrapf("invalid initiative ID: %s", identifier)
			}
			// Initiative existence check would require a RepKeeper method -- skip for now
		} else if contentType == "jury" {
			if identifier == "" {
				return types.ErrInvalidContentRef.Wrap("jury address cannot be empty")
			}
			// Address validation is sufficient
		} else {
			return types.ErrInvalidContentRef.Wrapf("unsupported rep content type: %s", contentType)
		}
	case "artifact":
		if contentType != "certificate" {
			return types.ErrInvalidContentRef.Wrapf("unsupported artifact content type: %s", contentType)
		}
		ids := strings.Split(identifier, "/")
		if len(ids) != 2 {
			return types.ErrInvalidContentRef.Wrapf("expected artifact/certificate/{class_id}/{edition_id}, got: %s", contentRef)
		}
		classID, err1 := strconv.ParseUint(ids[0], 10, 64)
		editionID, err2 := strconv.ParseUint(ids[1], 10, 64)
		if err1 != nil || err2 != nil {
			return types.ErrInvalidContentRef.Wrapf("invalid certificate ID: %s", identifier)
		}
		if k.artifactKeeper != nil {
			if !k.artifactKeeper.IsImpactCertificate(ctx, classID, editionID) {
				return types.ErrContentNotFound.Wrapf("impact certificate %d/%d not found", classID, editionID)
			}
		}
	default:
		return types.ErrInvalidContentRef.Wrapf("unsupported module: %s", module)
	}

	return nil
}

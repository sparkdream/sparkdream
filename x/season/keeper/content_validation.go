package keeper

import (
	"context"
	"strconv"
	"strings"

	reptypes "sparkdream/x/rep/types"
	"sparkdream/x/season/types"
)

// ValidateContentRef validates a content reference string.
// Supported formats: blog/post/{id}, forum/post/{id}, collect/collection/{id}, rep/initiative/{id}, rep/jury/{address},
// artifact/certificate/{class_id}/{edition_id}
func (k Keeper) ValidateContentRef(ctx context.Context, contentRef string) error {
	_, err := k.resolveContentRef(ctx, contentRef)
	return err
}

// parseCanonicalID parses a decimal id and rejects any spelling other than
// the canonical one (leading zeros, signs), so one piece of content has
// exactly one ref and the per-season duplicate check cannot be dodged with
// `blog/post/01` vs `blog/post/1` (SEA-08).
func parseCanonicalID(s string) (uint64, bool) {
	id, err := strconv.ParseUint(s, 10, 64)
	if err != nil || strconv.FormatUint(id, 10) != s {
		return 0, false
	}
	return id, true
}

// resolveContentRef validates a content ref and returns the content's author
// (creator, collection owner, assignee, juror or certificate creator) when
// the owning module can tell, or "" when it cannot (an unwired module).
//
// SEA-08: ids must be canonical; blog and forum posts and collections must be
// live (ACTIVE: not hidden, deleted, archived or pending); rep/initiative
// refs must name a completed initiative and rep/jury refs a member address,
// checked against x/rep.
func (k Keeper) resolveContentRef(ctx context.Context, contentRef string) (string, error) {
	parts := strings.Split(contentRef, "/")
	if len(parts) < 3 {
		return "", types.ErrInvalidContentRef.Wrapf("expected format: module/type/id, got: %s", contentRef)
	}

	module := parts[0]
	contentType := parts[1]
	identifier := strings.Join(parts[2:], "/") // for addresses that might contain "/"

	switch module {
	case "blog":
		if contentType != "post" {
			return "", types.ErrInvalidContentRef.Wrapf("unsupported blog content type: %s", contentType)
		}
		id, ok := parseCanonicalID(identifier)
		if !ok {
			return "", types.ErrInvalidContentRef.Wrapf("invalid blog post ID: %s", identifier)
		}
		if k.late.blogKeeper != nil {
			if !k.late.blogKeeper.HasPost(ctx, id) {
				return "", types.ErrContentNotFound.Wrapf("blog post %d not found", id)
			}
			createdAt, live := k.late.blogKeeper.LivePostCreatedAt(ctx, id)
			if !live {
				return "", types.ErrInvalidContentRef.Wrapf("blog post %d is hidden or deleted", id)
			}
			if err := k.checkCreatedThisSeason(ctx, createdAt, 0); err != nil {
				return "", err
			}
			if author, err := k.late.blogKeeper.GetPostAuthor(ctx, id); err == nil {
				return author, nil
			}
		}
	case "forum":
		if contentType != "post" {
			return "", types.ErrInvalidContentRef.Wrapf("unsupported forum content type: %s", contentType)
		}
		id, ok := parseCanonicalID(identifier)
		if !ok {
			return "", types.ErrInvalidContentRef.Wrapf("invalid forum post ID: %s", identifier)
		}
		if k.late.forumKeeper != nil {
			if !k.late.forumKeeper.HasPost(ctx, id) {
				return "", types.ErrContentNotFound.Wrapf("forum post %d not found", id)
			}
			createdAt, live := k.late.forumKeeper.LivePostCreatedAt(ctx, id)
			if !live {
				return "", types.ErrInvalidContentRef.Wrapf("forum post %d is hidden, deleted or archived", id)
			}
			if err := k.checkCreatedThisSeason(ctx, createdAt, 0); err != nil {
				return "", err
			}
			if author, err := k.late.forumKeeper.GetPostAuthor(ctx, id); err == nil {
				return author, nil
			}
		}
	case "collect":
		if contentType != "collection" {
			return "", types.ErrInvalidContentRef.Wrapf("unsupported collect content type: %s", contentType)
		}
		id, ok := parseCanonicalID(identifier)
		if !ok {
			return "", types.ErrInvalidContentRef.Wrapf("invalid collection ID: %s", identifier)
		}
		if k.late.collectKeeper != nil {
			if !k.late.collectKeeper.HasCollection(ctx, id) {
				return "", types.ErrContentNotFound.Wrapf("collection %d not found", id)
			}
			owner, createdHeight, live := k.late.collectKeeper.LiveCollection(ctx, id)
			if !live {
				return "", types.ErrInvalidContentRef.Wrapf("collection %d is not active", id)
			}
			if err := k.checkCreatedThisSeason(ctx, 0, createdHeight); err != nil {
				return "", err
			}
			return owner, nil
		}
	case "rep":
		switch contentType {
		case "initiative":
			id, ok := parseCanonicalID(identifier)
			if !ok {
				return "", types.ErrInvalidContentRef.Wrapf("invalid initiative ID: %s", identifier)
			}
			if k.late.repKeeper != nil {
				ini, err := k.late.repKeeper.GetInitiative(ctx, id)
				if err != nil {
					return "", types.ErrContentNotFound.Wrapf("initiative %d not found", id)
				}
				if ini.Status != reptypes.InitiativeStatus_INITIATIVE_STATUS_COMPLETED {
					return "", types.ErrInvalidContentRef.Wrapf("initiative %d is not completed (status %s)", id, ini.Status)
				}
				// SEA-30: only an initiative completed this season; an old
				// one cannot be nominated (and paid) every season.
				if ini.CompletedAt <= 0 {
					return "", types.ErrInvalidContentRef.Wrapf("initiative %d has no completion time", id)
				}
				if err := k.checkCreatedThisSeason(ctx, ini.CompletedAt, 0); err != nil {
					return "", err
				}
				return ini.Assignee, nil
			}
		case "jury":
			if identifier == "" {
				return "", types.ErrInvalidContentRef.Wrap("jury address cannot be empty")
			}
			bz, err := k.addressCodec.StringToBytes(identifier)
			if err != nil {
				return "", types.ErrInvalidContentRef.Wrapf("invalid jury address: %s", identifier)
			}
			if canon, err := k.addressCodec.BytesToString(bz); err != nil || canon != identifier {
				return "", types.ErrInvalidContentRef.Wrapf("non-canonical jury address: %s", identifier)
			}
			if k.late.repKeeper != nil && !k.late.repKeeper.IsMember(ctx, bz) {
				return "", types.ErrContentNotFound.Wrapf("jury address %s is not a member", identifier)
			}
			// SEA-30: the address must have completed a jury duty this
			// season (stamped by the jury hook), not merely be a member.
			if !k.servedJuryThisSeason(ctx, identifier) {
				return "", types.ErrInvalidContentRef.Wrapf("%s completed no jury duty this season", identifier)
			}
			return identifier, nil
		default:
			return "", types.ErrInvalidContentRef.Wrapf("unsupported rep content type: %s", contentType)
		}
	case "artifact":
		if contentType != "certificate" {
			return "", types.ErrInvalidContentRef.Wrapf("unsupported artifact content type: %s", contentType)
		}
		ids := strings.Split(identifier, "/")
		if len(ids) != 2 {
			return "", types.ErrInvalidContentRef.Wrapf("expected artifact/certificate/{class_id}/{edition_id}, got: %s", contentRef)
		}
		classID, ok1 := parseCanonicalID(ids[0])
		editionID, ok2 := parseCanonicalID(ids[1])
		if !ok1 || !ok2 {
			return "", types.ErrInvalidContentRef.Wrapf("invalid certificate ID: %s", identifier)
		}
		if k.late.artifactKeeper != nil {
			// SEA-30: the certificate's creator is its author (self-stake
			// ban, active-author check) and it must be from this season.
			creator, createdAt, ok := k.late.artifactKeeper.ImpactCertificateInfo(ctx, classID, editionID)
			if !ok {
				return "", types.ErrContentNotFound.Wrapf("impact certificate %d/%d not found", classID, editionID)
			}
			if err := k.checkCreatedThisSeason(ctx, createdAt, 0); err != nil {
				return "", err
			}
			return creator, nil
		}
	default:
		return "", types.ErrInvalidContentRef.Wrapf("unsupported module: %s", module)
	}

	return "", nil
}

// servedJuryThisSeason reports whether member completed a jury duty in the
// current season (MemberProfile.last_jury_duty_season, SEA-30).
func (k Keeper) servedJuryThisSeason(ctx context.Context, member string) bool {
	season, err := k.Season.Get(ctx)
	if err != nil {
		return false
	}
	profile, err := k.MemberProfile.Get(ctx, member)
	if err != nil {
		return false
	}
	return profile.JuryDutiesCompleted > 0 && profile.LastJuryDutySeason == season.Number
}

// checkCreatedThisSeason refuses content created before the current season
// started (SEA-08). Posts carry a unix creation time, compared with the
// season's start_time; collections a creation block height, compared with
// its start_block. A zero bound (season 1 from genesis, or no current
// season) admits everything.
func (k Keeper) checkCreatedThisSeason(ctx context.Context, createdAt, createdHeight int64) error {
	season, err := k.Season.Get(ctx)
	if err != nil {
		return nil
	}
	if createdAt > 0 && season.StartTime > 0 && createdAt < season.StartTime {
		return types.ErrInvalidContentRef.Wrapf("content created at %d, before season %d started (%d)", createdAt, season.Number, season.StartTime)
	}
	if createdHeight > 0 && season.StartBlock > 0 && createdHeight < season.StartBlock {
		return types.ErrInvalidContentRef.Wrapf("content created at block %d, before season %d started (block %d)", createdHeight, season.Number, season.StartBlock)
	}
	return nil
}
